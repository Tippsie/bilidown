package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"

	"bilidown/common"
	"bilidown/task"
	"bilidown/util"
)

func createTask(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if r.Method != http.MethodPost {
		util.Res{Success: false, Message: "不支持的请求方法"}.Write(w)
		return
	}
	var body []task.TaskInDB
	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		util.Res{Success: false, Message: "参数错误"}.Write(w)
		return
	}
	db := util.MustGetDB()
	defer db.Close()
	for _, item := range body {
		if !util.CheckBvidFormat(item.Bvid) {
			util.Res{Success: false, Message: "bvid 格式错误"}.Write(w)
			return
		}
		if item.Cover == "" || item.Title == "" || item.Owner == "" {
			util.Res{Success: false, Message: "参数错误"}.Write(w)
		}

		if !util.IsValidURL(item.Cover) {
			util.Res{Success: false, Message: "封面链接格式错误"}.Write(w)
			return
		}
		if !util.IsValidURL(item.Audio) {
			util.Res{Success: false, Message: "音频链接格式错误"}.Write(w)
			return
		}
		if !util.IsValidURL(item.Video) {
			util.Res{Success: false, Message: "视频链接格式错误"}.Write(w)
			return
		}
		if !util.IsValidFormatCode(item.Format) {
			util.Res{Success: false, Message: "清晰度代码错误"}.Write(w)
			return
		}
		item.Folder, err = util.GetCurrentFolder(db)
		item.Status = "waiting"
		if err != nil {
			util.Res{Success: false, Message: fmt.Sprintf("util.GetCurrentFolder: %v.", err)}.Write(w)
			return
		}
		_task := task.Task{TaskInDB: item}
		_task.CollectionTitle = util.FilterFileName(_task.CollectionTitle)
		_task.Title = util.FilterFileName(_task.Title)
		_task.Owner = util.FilterFileName(_task.Owner)
		err = _task.Create(db)
		if err != nil {
			util.Res{Success: false, Message: fmt.Sprintf("_task.Create: %v.", err)}.Write(w)
			return
		}
		go _task.Start()
	}
	util.Res{Success: true, Message: "创建成功"}.Write(w)
}

func getActiveTask(w http.ResponseWriter, r *http.Request) {
	util.Res{Success: true, Data: task.GlobalTaskList}.Write(w)
}

func getTaskList(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		util.Res{Success: false, Message: "参数错误"}.Write(w)
		return
	}
	db := util.MustGetDB()
	defer db.Close()
	page, err := strconv.Atoi(r.FormValue("page"))
	if err != nil {
		page = 0
	}
	pageSize, err := strconv.Atoi(r.FormValue("pageSize"))
	if err != nil {
		pageSize = 360
	}
	tasks, err := task.GetTaskList(db, page, pageSize)
	if err != nil {
		util.Res{Success: false, Message: err.Error()}.Write(w)
		return
	}
	util.Res{Success: true, Message: "获取成功", Data: tasks}.Write(w)
}

// showFile 调用 Explorer 查看文件位置
func showFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		util.Res{Success: false, Message: "参数错误"}.Write(w)
		return
	}
	filePath := r.FormValue("filePath")

	var cmd *exec.Cmd

	// 根据操作系统选择命令
	switch runtime.GOOS {
	case "windows":
		// Windows 使用 explorer
		cmd = exec.Command("explorer", "/select,", filePath)
	case "darwin":
		// macOS 使用 open
		cmd = exec.Command("open", "-R", filePath)
	case "linux":
		// Linux 使用 xdg-open
		cmd = exec.Command("xdg-open", filePath)
	default:
		util.Res{Success: false, Message: "不支持的操作系统"}.Write(w)
		return
	}
	err := cmd.Start()
	if err != nil {
		util.Res{Success: false, Message: err.Error()}.Write(w)
		return
	}
	util.Res{Success: true, Message: "操作成功"}.Write(w)
}

func clearFinishedTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		util.Res{Success: false, Message: "不支持的请求方法"}.Write(w)
		return
	}
	db := util.MustGetDB()
	defer db.Close()
	count, err := task.DeleteTasksByStatus(db, "done")
	if err != nil {
		util.Res{Success: false, Message: fmt.Sprintf("清除已完成任务失败: %v", err)}.Write(w)
		return
	}
	util.Res{Success: true, Message: fmt.Sprintf("已清除 %d 个已完成任务", count), Data: count}.Write(w)
}

func restartTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		util.Res{Success: false, Message: "不支持的请求方法"}.Write(w)
		return
	}
	db := util.MustGetDB()
	defer db.Close()

	var body []struct {
		ID           int64              `json:"id"`
		Format       common.MediaFormat `json:"format"`
		Audio        string             `json:"audio"`
		Video        string             `json:"video"`
		DownloadType string             `json:"downloadType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) == 0 {
		util.Res{Success: false, Message: "参数错误"}.Write(w)
		return
	}
	preparedTasks := make([]*task.Task, 0, len(body))
	for _, option := range body {
		item, err := task.GetTask(db, option.ID)
		if err != nil || item.Status != "error" && item.Status != "done" {
			util.Res{Success: false, Message: fmt.Sprintf("任务 %d 不存在或无法重启", option.ID)}.Write(w)
			return
		}
		if !util.IsValidFormatCode(option.Format) ||
			(option.DownloadType != "audio" && option.DownloadType != "video" && option.DownloadType != "merge") {
			util.Res{Success: false, Message: fmt.Sprintf("任务 %d 的下载设置无效", option.ID)}.Write(w)
			return
		}
		if option.DownloadType != "video" && !util.IsValidURL(option.Audio) {
			util.Res{Success: false, Message: fmt.Sprintf("任务 %d 的音频地址无效", option.ID)}.Write(w)
			return
		}
		if option.DownloadType != "audio" && !util.IsValidURL(option.Video) {
			util.Res{Success: false, Message: fmt.Sprintf("任务 %d 的视频地址无效", option.ID)}.Write(w)
			return
		}
		item.Format = option.Format
		item.Audio = option.Audio
		item.Video = option.Video
		item.DownloadType = option.DownloadType
		item.Status = "waiting"
		preparedTasks = append(preparedTasks, &task.Task{TaskInDB: *item})
	}
	for _, restartedTask := range preparedTasks {
		if err := restartedTask.PrepareRestart(db); err != nil {
			util.Res{Success: false, Message: fmt.Sprintf("更新任务 %d 状态失败: %v", restartedTask.ID, err)}.Write(w)
			return
		}
		go restartedTask.Start()
	}
	util.Res{Success: true, Message: fmt.Sprintf("已重启 %d 个任务", len(preparedTasks)), Data: len(preparedTasks)}.Write(w)
}

func stopTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		util.Res{Success: false, Message: "不支持的请求方法"}.Write(w)
		return
	}
	taskID, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		util.Res{Success: false, Message: "参数错误"}.Write(w)
		return
	}
	db := util.MustGetDB()
	defer db.Close()
	item, err := task.GetTask(db, taskID)
	if err != nil {
		util.Res{Success: false, Message: "任务不存在"}.Write(w)
		return
	}
	if item.Status == "done" {
		util.Res{Success: false, Message: "已完成任务无法停止"}.Write(w)
		return
	}
	if item.Status != "error" && !task.StopTask(taskID) {
		util.Res{Success: false, Message: "任务未能及时停止"}.Write(w)
		return
	}
	if err := (&task.Task{TaskInDB: *item}).UpdateStatus(db, "error"); err != nil {
		util.Res{Success: false, Message: fmt.Sprintf("停止任务失败: %v", err)}.Write(w)
		return
	}
	util.Res{Success: true, Message: "任务已停止"}.Write(w)
}

func deleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		util.Res{Success: false, Message: "不支持的请求方法"}.Write(w)
		return
	}
	taskID, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		util.Res{Success: false, Message: "参数错误"}.Write(w)
		return
	}
	db := util.MustGetDB()
	defer db.Close()
	item, err := task.GetTask(db, taskID)
	if err != nil {
		util.Res{Success: false, Message: "任务不存在"}.Write(w)
		return
	}
	if (item.Status == "waiting" || item.Status == "running") && !task.StopTask(taskID) {
		util.Res{Success: false, Message: "任务未能及时停止"}.Write(w)
		return
	}
	if err := task.DeleteTask(db, taskID); err != nil {
		util.Res{Success: false, Message: fmt.Sprintf("删除任务失败: %v", err)}.Write(w)
		return
	}
	task.RemoveActiveTask(taskID)
	util.Res{Success: true, Message: "任务已删除，下载文件已保留"}.Write(w)
}

func deleteAllTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		util.Res{Success: false, Message: "不支持的请求方法"}.Write(w)
		return
	}
	task.StopAllTasks()
	db := util.MustGetDB()
	defer db.Close()
	count, err := task.DeleteAllTasks(db)
	if err != nil {
		util.Res{Success: false, Message: fmt.Sprintf("删除全部任务失败: %v", err)}.Write(w)
		return
	}
	task.ClearActiveTasks()
	util.Res{Success: true, Message: fmt.Sprintf("已删除 %d 个任务，下载文件已保留", count), Data: count}.Write(w)
}
