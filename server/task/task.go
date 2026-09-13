package task

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"bilidown/bilibili"
	"bilidown/common"
	"bilidown/util"
)

// TaskInitOption 创建任务时需要从 POST 请求获取的参数
type TaskInitOption struct {
	Bvid            string             `json:"bvid"`
	Cid             int                `json:"cid"`
	Format          common.MediaFormat `json:"format"`
	CollectionTitle string             `json:"collectionTitle"`
	Title           string             `json:"title"`
	Owner           string             `json:"owner"`
	Cover           string             `json:"cover"`
	Status          TaskStatus         `json:"status"`
	Folder          string             `json:"folder"`
	Audio           string             `json:"audio"`
	Video           string             `json:"video"`
	Duration        int                `json:"duration"`
	DownloadType    string             `json:"downloadType"`
}

// TaskInDB 任务数据库中的数据
type TaskInDB struct {
	TaskInitOption
	ID       int64     `json:"id"`
	CreateAt time.Time `json:"createAt"`
}

func (task *TaskInDB) FilePath() string {
	ext := ".mp4"
	if task.DownloadType == "audio" {
		ext = ".m4a"
	}
	if task.CollectionTitle == "" {
		return filepath.Join(task.DownloadFolder(), fmt.Sprintf("%d%s%s", task.Cid, task.Title, ext))
	}
	return filepath.Join(task.DownloadFolder(), task.Title+ext)
}

func (task *TaskInDB) DownloadFolder() string {
	if task.CollectionTitle == "" {
		return filepath.Join(task.Folder, task.Bvid)
	}
	return filepath.Join(task.Folder, fmt.Sprintf("[%s][%s] %s", task.Bvid, task.Owner, task.CollectionTitle))
}

// done | waiting | running | error
type TaskStatus string

type Task struct {
	TaskInDB
	AudioProgress float64 `json:"audioProgress"`
	VideoProgress float64 `json:"videoProgress"`
	MergeProgress float64 `json:"mergeProgress"`
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
}

var GlobalTaskList = []*Task{}
var GlobalTaskMux = &sync.Mutex{}
var GlobalDownloadSem = util.NewSemaphore(3)
var GlobalMergeSem = util.NewSemaphore(3)

func (task *Task) Create(db *sql.DB) error {
	util.SqliteLock.Lock()
	result, err := db.Exec(`INSERT INTO "task" ("bvid", "cid", "format", "collection_title", "title", "owner", "cover", "status", "folder", "duration", "download_type")
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.Bvid,
		task.Cid,
		task.Format,
		task.CollectionTitle,
		task.Title,
		task.Owner,
		task.Cover,
		task.Status,
		task.Folder,
		task.Duration,
		task.DownloadType,
	)
	util.SqliteLock.Unlock()
	if err != nil {
		return err
	}

	task.ID, err = result.LastInsertId()
	task.CreateAt = time.Now()
	return err
}

// Create 创建任务，并将任务加入全局任务列表
func (task *Task) Start() {
	if task.DownloadType == "" {
		task.DownloadType = "merge"
	}
	task.ctx, task.cancel = context.WithCancel(context.Background())
	task.done = make(chan struct{})
	defer close(task.done)
	GlobalTaskMux.Lock()
	for index := len(GlobalTaskList) - 1; index >= 0; index-- {
		if GlobalTaskList[index].ID == task.ID {
			GlobalTaskList = append(GlobalTaskList[:index], GlobalTaskList[index+1:]...)
		}
	}
	GlobalTaskList = append(GlobalTaskList, task)
	GlobalTaskMux.Unlock()
	db := util.MustGetDB()
	defer db.Close()
	if err := os.MkdirAll(task.DownloadFolder(), os.ModePerm); err != nil {
		task.UpdateStatus(db, "error", fmt.Errorf("os.MkdirAll: %v", err))
		return
	}
	sessdata, err := bilibili.GetSessdata(db)
	if err != nil {
		task.UpdateStatus(db, "error", fmt.Errorf("bilibili.GetSessdata: %v", err))
		return
	}
	client := &bilibili.BiliClient{SESSDATA: sessdata}

	if !GlobalDownloadSem.AcquireContext(task.ctx) {
		task.UpdateStatus(db, "error", context.Canceled)
		return
	}
	task.UpdateStatus(db, "running")

	if task.DownloadType == "audio" {
		// 仅音频模式：只下载音频，重命名音频文件为输出文件
		err = DownloadMedia(client, task.Audio, task, "audio")
		if err != nil {
			GlobalDownloadSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("DownloadMedia: %v", err))
			return
		}
		GlobalDownloadSem.Release()
		if task.ctx.Err() != nil {
			task.UpdateStatus(db, "error", task.ctx.Err())
			return
		}
		outputPath := task.TaskInDB.FilePath()
		audioPath := filepath.Join(task.DownloadFolder(), strconv.FormatInt(task.ID, 10)+".audio")
		err = replaceFile(audioPath, outputPath)
		if err != nil {
			task.UpdateStatus(db, "error", fmt.Errorf("os.Rename: %v", err))
			return
		}
		// 添加元数据
		if err := task.addMetadata(outputPath); err != nil {
			log.Printf("添加元数据失败 (任务ID: %d): %v", task.ID, err)
		}
		if task.ctx.Err() != nil {
			task.UpdateStatus(db, "error", task.ctx.Err())
			return
		}
		task.UpdateStatus(db, "done")
		return
	} else if task.DownloadType == "video" {
		// 仅视频模式：只下载视频，重命名视频文件为输出文件
		err = DownloadMedia(client, task.Video, task, "video")
		if err != nil {
			GlobalDownloadSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("DownloadMedia: %v", err))
			return
		}
		GlobalDownloadSem.Release()
		if task.ctx.Err() != nil {
			task.UpdateStatus(db, "error", task.ctx.Err())
			return
		}
		outputPath := task.TaskInDB.FilePath()
		videoPath := filepath.Join(task.DownloadFolder(), strconv.FormatInt(task.ID, 10)+".video")
		err = replaceFile(videoPath, outputPath)
		if err != nil {
			task.UpdateStatus(db, "error", fmt.Errorf("os.Rename: %v", err))
			return
		}
		// 添加元数据
		if err := task.addMetadata(outputPath); err != nil {
			log.Printf("添加元数据失败 (任务ID: %d): %v", task.ID, err)
		}
		if task.ctx.Err() != nil {
			task.UpdateStatus(db, "error", task.ctx.Err())
			return
		}
		task.UpdateStatus(db, "done")
		return
	} else {
		// 合并模式：下载音频和视频，然后合并
		err = DownloadMedia(client, task.Audio, task, "audio")
		if err != nil {
			GlobalDownloadSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("DownloadMedia: %v", err))
			return
		}
		err = DownloadMedia(client, task.Video, task, "video")
		if err != nil {
			GlobalDownloadSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("DownloadMedia: %v", err))
			return
		}
		GlobalDownloadSem.Release()
		if task.ctx.Err() != nil {
			task.UpdateStatus(db, "error", task.ctx.Err())
			return
		}
		outputPath := task.TaskInDB.FilePath()
		videoPath := filepath.Join(task.DownloadFolder(), strconv.FormatInt(task.ID, 10)+".video")
		audioPath := filepath.Join(task.DownloadFolder(), strconv.FormatInt(task.ID, 10)+".audio")
		mergedPath := filepath.Join(task.DownloadFolder(), strconv.FormatInt(task.ID, 10)+".merge.mp4")
		if !GlobalMergeSem.AcquireContext(task.ctx) {
			task.UpdateStatus(db, "error", context.Canceled)
			return
		}
		err = task.MergeMedia(mergedPath, videoPath, audioPath)
		if err != nil {
			GlobalMergeSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("task.MergeMedia: %v", err))
			return
		}
		if err = replaceFile(mergedPath, outputPath); err != nil {
			GlobalMergeSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("replaceFile: %v", err))
			return
		}
		err = os.Remove(videoPath)
		if err != nil {
			GlobalMergeSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("os.Remove: %v", err))
			return
		}
		err = os.Remove(audioPath)
		if err != nil {
			GlobalMergeSem.Release()
			task.UpdateStatus(db, "error", fmt.Errorf("os.Remove: %v", err))
			return
		}
		GlobalMergeSem.Release()
		// 添加元数据
		if err := task.addMetadata(outputPath); err != nil {
			log.Printf("添加元数据失败 (任务ID: %d): %v", task.ID, err)
		}
		if task.ctx.Err() != nil {
			task.UpdateStatus(db, "error", task.ctx.Err())
			return
		}
		task.UpdateStatus(db, "done")
	}
}

// 合并音视频
func (task *Task) MergeMedia(outputPath string, inputPaths ...string) error {
	inputs := []string{}
	for _, path := range inputPaths {
		inputs = append(inputs, "-i", path)
	}

	ffmpegPath, err := util.GetFFmpegPath()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(task.ctx, ffmpegPath, append(inputs, "-c:v", "copy", "-c:a", "copy", "-progress", "pipe:1", "-strict", "-2", "-y", outputPath)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)

	progress := newProgressBar(int64(task.Duration))
	outTimeRegex := regexp.MustCompile(`out_time_ms=(\d+)`) // 毫秒

	for scanner.Scan() {
		line := scanner.Text()
		match := outTimeRegex.FindStringSubmatch(line)
		if len(match) == 2 {
			outTime, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				return err
			}
			progress.current = outTime / 1000000
			task.MergeProgress = progress.percent()
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if err := cmd.Wait(); err != nil {
		return err
	}
	task.MergeProgress = 1
	return nil
}

func GetVideoURL(medias []bilibili.Media, format common.MediaFormat) (string, error) {
	for _, code := range []int{12, 7, 13} {
		for _, item := range medias {
			if item.ID == format && item.Codecid == code {
				return item.BaseURL, nil
			}
		}
	}
	return "", errors.New("未找到对应视频分辨率格式")
}

func GetAudioURL(dash *bilibili.Dash) string {
	if dash.Flac != nil {
		return dash.Flac.Audio.BaseURL
	}
	var maxAudioID common.MediaFormat
	var audioURL string
	for _, item := range dash.Audio {
		if item.ID > maxAudioID {
			maxAudioID = item.ID
			audioURL = item.BaseURL
		}
	}
	return audioURL
}

func (task *Task) UpdateStatus(db *sql.DB, status TaskStatus, errs ...error) error {
	util.SqliteLock.Lock()
	_, err := db.Exec(`UPDATE "task" SET "status" = ? WHERE "id" = ?`, status, task.ID)
	util.SqliteLock.Unlock()
	if err != nil {
		return err
	}
	for _, err := range errs {
		if err != nil {
			err = util.CreateLog(db, fmt.Sprintf("Task-%d-Error: %v", task.ID, err))
			if err != nil {
				log.Fatalln("CreateLog:", err)
			}
		}
	}
	task.Status = status
	return err
}

func DownloadMedia(client *bilibili.BiliClient, _url string, task *Task, mediaType string) error {
	var resp *http.Response
	var err error
	for i := 0; i < 5; i++ {
		resp, err = client.SimpleGETContext(task.ctx, _url, nil)
		if err == nil {
			break
		}
	}

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	filename := strconv.FormatInt(task.ID, 10) + "." + mediaType
	filepath := filepath.Join(task.DownloadFolder(), filename)

	progress := newProgressBar(resp.ContentLength)

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := io.TeeReader(resp.Body, file)
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		progress.add(n)
		GlobalTaskMux.Lock()
		if mediaType == "video" {
			task.VideoProgress = progress.percent()
		} else {
			task.AudioProgress = progress.percent()
		}
		GlobalTaskMux.Unlock()
	}
	return nil
}

type progressBar struct {
	total   int64
	current int64
}

func (p *progressBar) add(n int) {
	p.current += int64(n)
}

func (p *progressBar) percent() float64 {
	return float64(p.current) / float64(p.total)
}

func newProgressBar(total int64) *progressBar {
	return &progressBar{
		total: total,
	}
}

func GetTaskList(db *sql.DB, page int, pageSize int) ([]TaskInDB, error) {
	tasks := []TaskInDB{}
	util.SqliteLock.Lock()
	rows, err := db.Query(`SELECT
		"id", "bvid", "cid", "format", "collection_title", "title",
		"owner", "cover", "status", "folder", "duration", "download_type", "create_at"
	FROM "task" ORDER BY "id" DESC LIMIT ?, ?`,
		page*pageSize, pageSize,
	)
	util.SqliteLock.Unlock()
	if err != nil {
		return nil, err
	}

	createAt := ""

	for rows.Next() {
		task := TaskInDB{}
		err = rows.Scan(
			&task.ID,
			&task.Bvid,
			&task.Cid,
			&task.Format,
			&task.CollectionTitle,
			&task.Title,
			&task.Owner,
			&task.Cover,
			&task.Status,
			&task.Folder,
			&task.Duration,
			&task.DownloadType,
			&createAt,
		)
		if err != nil {
			return nil, err
		}
		task.CreateAt, err = time.Parse("2006-01-02 15:04:05", createAt)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func DeleteTask(db *sql.DB, taskID int64) error {
	util.SqliteLock.Lock()
	_, err := db.Exec(`DELETE FROM "task" WHERE "id" = ?`, taskID)
	util.SqliteLock.Unlock()
	return err
}

func GetTask(db *sql.DB, taskID int64) (*TaskInDB, error) {
	task := TaskInDB{}
	createAt := ""
	util.SqliteLock.Lock()
	err := db.QueryRow(`SELECT
		"id", "bvid", "cid", "format", "collection_title", "title",
		"owner", "cover", "status", "folder", "duration", "download_type", "create_at"
	FROM "task" WHERE "id" = ?`,
		taskID,
	).Scan(
		&task.ID,
		&task.Bvid,
		&task.Cid,
		&task.Format,
		&task.CollectionTitle,
		&task.Title,
		&task.Owner,
		&task.Cover,
		&task.Status,
		&task.Folder,
		&task.Duration,
		&task.DownloadType,
		&createAt,
	)
	util.SqliteLock.Unlock()
	if err != nil {
		return nil, err
	}

	task.CreateAt, err = time.Parse("2006-01-02 15:04:05", createAt)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// addMetadata 使用 ffmpeg 给输出文件添加元数据（description 和 artist）
func (task *Task) addMetadata(filePath string) error {
	ffmpegPath, err := util.GetFFmpegPath()
	if err != nil {
		return err
	}

	desc := task.Bvid
	if desc == "" {
		desc = ""
	}

	author := task.Owner

	// 临时文件加上 .mp4 扩展名
	tempPath := filePath + ".tmp.mp4"

	// 使用双引号包裹文件路径，避免特殊字符
	cmd := exec.CommandContext(task.ctx, ffmpegPath,
		"-i", filePath,
		"-metadata", "description="+desc,
		"-metadata", "artist="+author,
		"-codec", "copy",
		"-y",
		tempPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg添加元数据失败: %v, 输出: %s", err, string(output))
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除原文件失败: %v", err)
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		return fmt.Errorf("重命名临时文件失败: %v", err)
	}

	return nil
}

func DeleteAllTasks(db *sql.DB) (int64, error) {
	util.SqliteLock.Lock()
	result, err := db.Exec(`DELETE FROM "task"`)
	util.SqliteLock.Unlock()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (task *Task) Stop() {
	if task.cancel != nil {
		task.cancel()
	}
}

func StopTask(taskID int64) bool {
	GlobalTaskMux.Lock()
	var activeTask *Task
	for _, item := range GlobalTaskList {
		if item.ID == taskID {
			activeTask = item
			break
		}
	}
	GlobalTaskMux.Unlock()
	if activeTask == nil {
		return false
	}
	activeTask.Stop()
	select {
	case <-activeTask.done:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

func StopAllTasks() {
	GlobalTaskMux.Lock()
	activeTasks := append([]*Task(nil), GlobalTaskList...)
	GlobalTaskMux.Unlock()
	for _, item := range activeTasks {
		if item.Status == "waiting" || item.Status == "running" {
			item.Stop()
		}
	}
	for _, item := range activeTasks {
		if item.done == nil || item.Status != "waiting" && item.Status != "running" {
			continue
		}
		select {
		case <-item.done:
		case <-time.After(10 * time.Second):
		}
	}
}

func RemoveActiveTask(taskID int64) {
	GlobalTaskMux.Lock()
	defer GlobalTaskMux.Unlock()
	for index := len(GlobalTaskList) - 1; index >= 0; index-- {
		if GlobalTaskList[index].ID == taskID {
			GlobalTaskList = append(GlobalTaskList[:index], GlobalTaskList[index+1:]...)
		}
	}
}

func ClearActiveTasks() {
	GlobalTaskMux.Lock()
	GlobalTaskList = []*Task{}
	GlobalTaskMux.Unlock()
}

func replaceFile(sourcePath string, destinationPath string) error {
	backupPath := destinationPath + ".restart-backup"
	_ = os.Remove(backupPath)
	if _, err := os.Stat(destinationPath); err == nil {
		if err := os.Rename(destinationPath, backupPath); err != nil {
			return err
		}
	}
	if err := os.Rename(sourcePath, destinationPath); err != nil {
		_ = os.Rename(backupPath, destinationPath)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func (task *Task) PrepareRestart(db *sql.DB) error {
	util.SqliteLock.Lock()
	_, err := db.Exec(`UPDATE "task" SET "format" = ?, "download_type" = ?, "status" = 'waiting' WHERE "id" = ?`,
		task.Format, task.DownloadType, task.ID)
	util.SqliteLock.Unlock()
	return err
}

func DeleteTasksByStatus(db *sql.DB, status TaskStatus) (int64, error) {
	util.SqliteLock.Lock()
	result, err := db.Exec(`DELETE FROM "task" WHERE "status" = ?`, status)
	util.SqliteLock.Unlock()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
