import van, { State } from 'vanjs-core'
import { Modal } from 'bootstrap'
import PQueue from 'p-queue'
import { TaskInDB, PlayInfo, VideoFormat } from '../work/type'
import { getPlayInfo } from '../work/data'
import { getActiveFormatVideo, getAudioURL, videoFormatMap } from '../work/view/parseModal'
import { restartTasks, RestartTaskData } from './data'
import { VanComponent } from '../mixin'

const { button, div, input, label, option, select } = van.tags

type RetryItem = {
    task: TaskInDB
    info: PlayInfo
    selected: State<boolean>
    formatIndex: State<number>
}

export class RestartModalComp implements VanComponent {
    element: HTMLElement
    modal: Modal
    sourceTasks: TaskInDB[] = []
    items: State<RetryItem[]> = van.state([])
    errors: State<string[]> = van.state([])
    loading = van.state(false)
    submitting = van.state(false)
    downloadType = van.state<'audio' | 'video' | 'merge'>('merge')
    preferredCodec = van.state<12 | 7 | 13>(12)
    preferHiResAudio = van.state(true)
    controllers: AbortController[] = []

    constructor(private onRestarted: (ids: number[]) => void) {
        this.element = this.Root()
        this.modal = new Modal(this.element)
        this.element.addEventListener('hidden.bs.modal', () => this.reset())
    }

    show(tasks: TaskInDB[]) {
        this.sourceTasks = tasks
        if (tasks.length > 0) this.downloadType.val = tasks[0].downloadType
        this.modal.show()
        this.parse()
    }

    async parse() {
        this.loading.val = true
        this.items.val = []
        this.errors.val = []
        const queue = new PQueue({ concurrency: 10 })
        for (const task of this.sourceTasks) {
            queue.add(async () => {
                const controller = new AbortController()
                this.controllers.push(controller)
                const info = await getPlayInfo(task.bvid, task.cid, controller)
                info.accept_quality = [...new Set(info.dash.video.map(video => video.id))].sort((a, b) => b - a)
                const currentIndex = info.accept_quality.indexOf(task.format as VideoFormat)
                this.items.val = this.items.val.concat({
                    task,
                    info,
                    selected: van.state(true),
                    formatIndex: van.state(currentIndex >= 0 ? currentIndex : 0)
                })
            }).catch(() => {
                this.errors.val = this.errors.val.concat(task.title)
            })
        }
        await queue.onIdle()
        this.loading.val = false
    }

    async restart() {
        this.submitting.val = true
        try {
            const selected = this.items.val.filter(item => item.selected.val)
            const payload: RestartTaskData[] = selected.map(item => {
                const format = item.info.accept_quality[item.formatIndex.val]
                return {
                    id: item.task.id,
                    format,
                    downloadType: this.downloadType.val,
                    audio: this.downloadType.val == 'video' ? '' : getAudioURL(item.info, this.preferHiResAudio.val),
                    video: this.downloadType.val == 'audio' ? '' : getActiveFormatVideo(item.info, format, this.preferredCodec.val).video
                }
            })
            await restartTasks(payload)
            this.onRestarted(selected.map(item => item.task.id))
            this.modal.hide()
        } catch (error) {
            if (error instanceof Error) alert(error.message)
        } finally {
            this.submitting.val = false
        }
    }

    reset() {
        this.controllers.forEach(controller => controller.abort())
        this.controllers = []
        this.sourceTasks = []
        this.items.val = []
        this.errors.val = []
        this.loading.val = false
    }

    Root() {
        const selectedCount = van.derive(() => this.items.val.filter(item => item.selected.val).length)
        const allSelected = van.derive(() => this.items.val.length > 0 && selectedCount.val == this.items.val.length)
        return div({ class: 'modal fade', tabIndex: -1 },
            div({ class: 'modal-dialog modal-xl modal-fullscreen-xl-down modal-dialog-scrollable' },
                div({ class: 'modal-content' },
                    div({ class: 'modal-header' },
                        div({ class: 'h5 modal-title' }, () => this.loading.val ? '批量解析' : '批量下载'),
                        button({ class: 'btn-close', 'data-bs-dismiss': 'modal' })
                    ),
                    div({ class: 'modal-body vstack gap-3' },
                        div({ class: 'vstack gap-3', hidden: () => !this.loading.val },
                            div({ class: 'text-center fs-5' }, () => `正在解析，剩余 ${this.sourceTasks.length - this.items.val.length - this.errors.val.length} 项`),
                            div({ class: 'progress' },
                                div({
                                    class: 'progress-bar progress-bar-striped progress-bar-animated',
                                    style: () => `width: ${(this.items.val.length + this.errors.val.length) / this.sourceTasks.length * 100}%`
                                })
                            )
                        ),
                        div({ class: 'vstack gap-2', hidden: () => this.loading.val || this.errors.val.length == 0 },
                            div({ class: 'text-danger' }, () => `以下 ${this.errors.val.length} 个视频解析失败`),
                            () => div({ class: 'list-group' }, this.errors.val.map(title => div({ class: 'list-group-item disabled' }, title)))
                        ),
                        () => div({ class: 'list-group', hidden: this.loading },
                            this.items.val.map(item => div({
                                class: 'list-group-item user-select-none py-0',
                                role: 'button',
                                onclick(event) {
                                    if ((event.target as HTMLElement).getAttribute('class')?.match(/dropdown-?/)) return
                                    item.selected.val = !item.selected.val
                                }
                            },
                                div({ class: 'hstack gap-2' },
                                    div({ class: 'hstack gap-3 flex-fill py-1' },
                                        input({ class: 'form-check-input', type: 'checkbox', checked: item.selected }),
                                        div(item.task.title)
                                    ),
                                    div({ class: 'dropdown' },
                                        div({ class: 'dropdown-toggle py-2 text-primary', 'data-bs-toggle': 'dropdown' },
                                            () => videoFormatMap[item.info.accept_quality[item.formatIndex.val]]
                                        ),
                                        () => div({ class: 'dropdown-menu shadow' },
                                            item.info.accept_quality.map((format, index) => div({
                                                class: () => `dropdown-item ${item.formatIndex.val == index ? 'active' : ''}`,
                                                onclick() { item.formatIndex.val = index }
                                            }, videoFormatMap[format]))
                                        )
                                    )
                                )
                            ))
                        )
                    ),
                    div({ class: 'modal-footer' },
                        div({ class: 'me-auto hstack gap-3 text-nowrap', hidden: this.loading },
                            () => `已选择 (${selectedCount.val}/${this.items.val.length}) 项`,
                            select({
                                class: 'form-select form-select-sm', value: this.downloadType,
                                oninput: event => this.downloadType.val = (event.target as HTMLSelectElement).value as 'audio' | 'video' | 'merge'
                            },
                                option({ value: 'merge' }, '音视频合并'),
                                option({ value: 'audio' }, '仅音频'),
                                option({ value: 'video' }, '仅视频')
                            ),
                            select({
                                class: 'form-select form-select-sm', value: this.preferredCodec,
                                oninput: event => this.preferredCodec.val = Number((event.target as HTMLSelectElement).value) as 12 | 7 | 13
                            },
                                option({ value: '12' }, 'HEVC (hev1)'),
                                option({ value: '7' }, 'AVC (avc1)'),
                                option({ value: '13' }, 'AV1 (av01)')
                            ),
                            div({ class: 'form-check form-check-inline' },
                                input({
                                    type: 'checkbox', class: 'form-check-input', id: 'restartPreferHiResAudio',
                                    checked: this.preferHiResAudio,
                                    oninput: event => this.preferHiResAudio.val = (event.target as HTMLInputElement).checked
                                }),
                                label({ class: 'form-check-label', for: 'restartPreferHiResAudio' }, 'Hi-Res')
                            )
                        ),
                        button({ class: 'btn btn-secondary', 'data-bs-dismiss': 'modal' }, '取消'),
                        button({
                            class: 'btn btn-secondary',
                            hidden: () => this.loading.val || allSelected.val || this.items.val.length == 0,
                            onclick: () => this.items.val.forEach(item => item.selected.val = true)
                        }, '全选'),
                        button({
                            class: 'btn btn-warning',
                            hidden: () => this.loading.val || !allSelected.val,
                            onclick: () => this.items.val.forEach(item => item.selected.val = false)
                        }, '全不选'),
                        button({
                            class: 'btn btn-primary',
                            hidden: this.loading,
                            disabled: () => selectedCount.val == 0 || this.submitting.val,
                            onclick: () => this.restart()
                        }, () => this.submitting.val ? '正在重启...' : '开始下载')
                    )
                )
            )
        )
    }
}
