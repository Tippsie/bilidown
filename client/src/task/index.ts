import van, { State } from 'vanjs-core'
import { Route, goto, now } from 'vanjs-router'
import { checkLogin, GLOBAL_HAS_LOGIN, GLOBAL_HIDE_PAGE, ResJSON, VanComponent } from '../mixin'
import { clearFinishedTasks, deleteAllTasks, deleteTask, getActiveTask, getTaskList, showFile, stopTask } from './data'
import { TaskInDB, TaskStatus } from '../work/type'
import { LoadingBox } from '../view'
import { PlayerModalComp } from './playerModal'
import { RestartModalComp } from './restartModal'

const { button, div, span } = van.tags

const { svg, path, rect } = van.tags('http://www.w3.org/2000/svg')

export class TaskRoute implements VanComponent {
    element: HTMLElement
    /** 包含视频播放器的模态框 */
    playerModalComp = new PlayerModalComp()
    restartModalComp = new RestartModalComp(ids => {
        this.taskList.val.forEach(task => {
            if (!ids.includes(task.id)) return
            task.statusState.val = 'waiting'
            task.audioProgress.val = 0
            task.videoProgress.val = 0
            task.mergeProgress.val = 0
        })
        this.startRefresh()
    })

    loading = van.state(false)

    taskList: State<(TaskInDB & {
        /** 音频下载进度百分比 */
        audioProgress: State<number>
        /** 视频下载进度百分比 */
        videoProgress: State<number>
        /** 合并进度百分比 */
        mergeProgress: State<number>
        /** 任务状态 */
        statusState: State<TaskStatus>
        /** 是否正在打开 */
        opening: State<boolean>
        /** 是否正在停止 */
        stopping: State<boolean>
        /** 是否正在删除 */
        deleting: State<boolean>
    })[]> = van.state([])

    clearing = van.state(false)
    deletingAll = van.state(false)
    refreshTimer: ReturnType<typeof setInterval> | undefined
    refreshHelper: ReturnType<typeof setInterval> | undefined

    constructor() {
        van.add(document.body, this.restartModalComp.element)
        this.element = this.Root()
    }

    Root() {
        const _that = this
        return Route({
            rule: 'task',
            Loader() {
                return div(
                    () => _that.loading.val ? LoadingBox() : '',
                    div({ class: 'hstack justify-content-end gap-2 mb-3' },
                        button({
                            class: 'btn btn-outline-secondary btn-sm',
                            disabled: () => _that.clearing.val || !_that.taskList.val.some(task => task.statusState.val == 'done'),
                            async onclick() {
                                _that.clearing.val = true
                                try {
                                    await clearFinishedTasks()
                                    _that.taskList.val = _that.taskList.val.filter(task => task.statusState.val != 'done')
                                } catch (error) {
                                    if (error instanceof Error) alert(error.message)
                                } finally {
                                    _that.clearing.val = false
                                }
                            }
                        }, () => _that.clearing.val ? '正在清除...' : '清除已完成'),
                        button({
                            class: 'btn btn-primary btn-sm',
                            disabled: () => !_that.taskList.val.some(task => task.statusState.val == 'error' || task.statusState.val == 'done'),
                            onclick() {
                                _that.restartModalComp.show(
                                    _that.taskList.val.filter(task => task.statusState.val == 'error' || task.statusState.val == 'done')
                                )
                            }
                        }, '全部重启'),
                        button({
                            class: 'btn btn-danger btn-sm',
                            disabled: () => _that.deletingAll.val || _that.taskList.val.length == 0,
                            async onclick() {
                                if (!confirm('确定要删除全部任务吗？下载文件会保留在磁盘上。')) return
                                _that.deletingAll.val = true
                                try {
                                    await deleteAllTasks()
                                    _that.taskList.val = []
                                } catch (error) {
                                    if (error instanceof Error) alert(error.message)
                                } finally {
                                    _that.deletingAll.val = false
                                }
                            }
                        }, () => _that.deletingAll.val ? '正在删除...' : '全部删除'),
                    ),
                    () => div({ class: 'list-group', hidden: _that.loading.val },
                        _that.taskList.val.map(task => {
                            const ext = task.downloadType === 'audio' ? '.m4a' : '.mp4'
                            const filename = task.collectionTitle ? `${task.title}${ext}` : `${task.cid}${task.title}${ext}`
                            const downloadFolder = task.collectionTitle
                                ? `${task.folder}/[${task.bvid}][${task.owner}] ${task.collectionTitle}`
                                : `${task.folder}/${task.bvid}`
                            return div({
                                class: 'list-group-item p-0 hstack user-select-none',
                            },
                                div({
                                    class: 'vstack gap-2 py-2 px-3',
                                    style: `cursor: pointer;`,
                                    onclick() {
                                        const src = `/api/downloadVideo?path=${encodeURIComponent(
                                            `${downloadFolder}/${filename}`
                                        )}`
                                        if (task.statusState.val != 'done') return
                                        _that.playerModalComp.open(src, task.title, task.downloadType === 'audio' ? 'audio' : 'video')
                                    }
                                },
                                    div({
                                        class: () => `
                                        ${task.statusState.val == 'error' ? 'text-danger' : ''}
                                        ${task.statusState.val == 'waiting' || task.statusState.val == 'running'
                                                ? 'text-primary' : ''}`
                                    },
                                        () => {
                                            if (task.opening.val) return '正在打开文件位置...'
                                            return div(
                                                span({
                                                    class: `me-2 badge ${task.downloadType === 'audio' ? 'bg-success' : 'bg-primary'}`,
                                                    title: task.downloadType === 'audio' ? '音频' : '视频'
                                                }, task.downloadType === 'audio' ? 'A' : 'V'),
                                                span({}, filename),
                                            )
                                        }),
                                    div({ class: 'text-secondary small' },
                                        () => {
                                            if (task.statusState.val == 'waiting') return '等待下载'
                                            if (task.statusState.val == 'error') return '下载失败'
                                            if (task.statusState.val == 'done') return downloadFolder
                                            if (task.videoProgress.val == 0) {
                                                return `正在下载音频 (${(task.audioProgress.val * 100).toFixed(2)}%)`
                                            } else if (task.mergeProgress.val == 0) {
                                                return `正在下载视频 (${(task.videoProgress.val * 100).toFixed(2)}%)`
                                            } else if (task.statusState.val == 'running') {
                                                return `正在合并音视频 (${(task.mergeProgress.val * 100).toFixed(2)}%)`
                                            } else {
                                                return downloadFolder
                                            }
                                        }
                                    ),
                                    div({
                                        class: `progress`,
                                        style: `height: 5px`,
                                        hidden: () => task.statusState.val == 'done' || task.statusState.val == 'error'
                                    },
                                        div({
                                            class: () => `progress-bar progress-bar-striped progress-bar-animated bg-${(() => {
                                                if (task.videoProgress.val == 0) return 'primary'
                                                if (task.mergeProgress.val == 0) return 'success'
                                                else return 'info'
                                            })()}`,
                                            style: () => {
                                                let width = 0
                                                if (task.videoProgress.val == 0) width = task.audioProgress.val * 100
                                                else if (task.mergeProgress.val == 0) width = task.videoProgress.val * 100
                                                else width = task.mergeProgress.val * 100
                                                return `width: ${width}%`
                                            }
                                        }),
                                    )
                                ),
                                div({
                                    class: 'me-4',
                                    hidden: task.statusState.val != 'done'
                                        || task.opening.val
                                },
                                    div({
                                        class: 'hover-btn', title: '打开文件位置',
                                        onclick() {
                                            showFile(`${downloadFolder}/${filename}`)
                                            task.opening.val = true
                                            setTimeout(() => {
                                                task.opening.val = false
                                            }, 3000)
                                        }
                                    },
                                        _that.FolderSVG()
                                    )
                                ),
                                button({
                                    type: 'button',
                                    class: 'btn btn-link me-4 p-0 text-primary',
                                    title: '重启任务',
                                    'aria-label': '重启任务',
                                    hidden: () => task.statusState.val != 'error' && task.statusState.val != 'done',
                                    onclick() {
                                        _that.restartModalComp.show([task])
                                    }
                                }, _that.RotateCwSVG()),
                                button({
                                    type: 'button',
                                    class: 'btn btn-link me-4 p-0 text-danger',
                                    title: '停止任务',
                                    'aria-label': '停止任务',
                                    hidden: () => task.statusState.val == 'done',
                                    disabled: task.stopping,
                                    async onclick() {
                                        task.stopping.val = true
                                        try {
                                            await stopTask(task.id)
                                            task.statusState.val = 'error'
                                        } catch (error) {
                                            if (error instanceof Error) alert(error.message)
                                        } finally {
                                            task.stopping.val = false
                                        }
                                    }
                                }, _that.StopSVG()),
                                button({
                                    type: 'button',
                                    class: 'btn btn-link me-4 p-0 text-danger',
                                    title: '删除任务',
                                    'aria-label': '删除任务',
                                    disabled: task.deleting,
                                    async onclick() {
                                        if (!confirm('确定要删除此任务吗？下载文件会保留在磁盘上。')) return
                                        task.deleting.val = true
                                        try {
                                            await deleteTask(task.id)
                                            _that.taskList.val = _that.taskList.val.filter(item => item.id != task.id)
                                        } catch (error) {
                                            if (error instanceof Error) alert(error.message)
                                        } finally {
                                            task.deleting.val = false
                                        }
                                    }
                                }, _that.TrashSVG()),
                            )
                        })
                    )
                )
            },
            async onFirst() {
                if (!await checkLogin()) return
            },
            async onLoad() {
                if (!GLOBAL_HAS_LOGIN.val) return goto('login')
                _that.loading.val = true

                getTaskList(0, 360).then(taskList => {
                    if (!taskList) return
                    _that.taskList.val = taskList.map(task => ({
                        ...task,
                        audioProgress: van.state(1),
                        videoProgress: van.state(1),
                        mergeProgress: van.state(1),
                        statusState: van.state(task.status),
                        opening: van.state(false),
                        stopping: van.state(false),
                        deleting: van.state(false),
                    }))
                    _that.startRefresh()
                })
            },
        })
    }

    startRefresh() {
        if (this.refreshTimer) clearInterval(this.refreshTimer)
        if (this.refreshHelper) clearInterval(this.refreshHelper)
        const refresh = async () => {
            const activeTaskList = await getActiveTask()
            if (!activeTaskList) return
            setTimeout(() => this.loading.val = false, 200)
            this.taskList.val.forEach(taskInDB => {
                activeTaskList.forEach(task => {
                    if (taskInDB.id != task.id) return
                    taskInDB.audioProgress.val = task.audioProgress
                    taskInDB.videoProgress.val = task.videoProgress
                    taskInDB.mergeProgress.val = task.mergeProgress
                    taskInDB.statusState.val = task.status
                })
            })
            if (!activeTaskList.some(task => task.status == 'waiting' || task.status == 'running')) {
                if (this.refreshTimer) clearInterval(this.refreshTimer)
                if (this.refreshHelper) clearInterval(this.refreshHelper)
            }
        }
        refresh()
        this.refreshTimer = setInterval(refresh, 1000)
        this.refreshHelper = setInterval(() => {
            if (now.val.split('/')[0] != 'task') {
                if (this.refreshHelper) clearInterval(this.refreshHelper)
                if (this.refreshTimer) clearInterval(this.refreshTimer)
            }
        }, 1000)
    }

    FolderSVG() {
        return svg({ style: `width: 1em; height: 1em`, fill: "currentColor", class: "bi bi-folder2", viewBox: "0 0 16 16" },
            path({ "d": "M1 3.5A1.5 1.5 0 0 1 2.5 2h2.764c.958 0 1.76.56 2.311 1.184C7.985 3.648 8.48 4 9 4h4.5A1.5 1.5 0 0 1 15 5.5v7a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 1 12.5zM2.5 3a.5.5 0 0 0-.5.5V6h12v-.5a.5.5 0 0 0-.5-.5H9c-.964 0-1.71-.629-2.174-1.154C6.374 3.334 5.82 3 5.264 3zM14 7H2v5.5a.5.5 0 0 0 .5.5h11a.5.5 0 0 0 .5-.5z" }),
        )
    }

    RotateCwSVG() {
        return svg({
            width: 20,
            height: 20,
            viewBox: '0 0 24 24',
            fill: 'none',
            stroke: 'currentColor',
            'stroke-width': 2,
            'stroke-linecap': 'round',
            'stroke-linejoin': 'round',
            'aria-hidden': 'true'
        },
            path({ d: 'M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1.06 6.64 2.93L21 8' }),
            path({ d: 'M21 3v5h-5' })
        )
    }

    StopSVG() {
        return svg({
            width: 20,
            height: 20,
            viewBox: '0 0 24 24',
            fill: 'none',
            stroke: 'currentColor',
            'stroke-width': 2,
            'stroke-linecap': 'round',
            'stroke-linejoin': 'round',
            'aria-hidden': 'true'
        }, rect({ width: 14, height: 14, x: 5, y: 5, rx: 1 }))
    }

    TrashSVG() {
        return svg({
            width: 20,
            height: 20,
            viewBox: '0 0 24 24',
            fill: 'none',
            stroke: 'currentColor',
            'stroke-width': 2,
            'stroke-linecap': 'round',
            'stroke-linejoin': 'round',
            'aria-hidden': 'true'
        },
            path({ d: 'M3 6h18' }),
            path({ d: 'M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6' }),
            path({ d: 'M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2' }),
            path({ d: 'M10 11v6' }),
            path({ d: 'M14 11v6' })
        )
    }
}

export default () => new TaskRoute().element
