package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"scrcpy-lan/webui/internal/task"
)

// handleBatch 处理 /api/devices/batch/{kind}：exec / install / push。
func (a *api) handleBatch(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimPrefix(r.URL.Path, "/api/devices/batch/")
	switch kind {
	case "exec":
		a.batchExec(w, r)
	case "install":
		a.batchInstall(w, r)
	case "push":
		a.batchPush(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (a *api) batchExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IDs []string `json:"ids"`
		Cmd string   `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Cmd == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	results := task.ExecBatch(r.Context(), a.mgr, req.IDs, req.Cmd)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// batchInstall 处理 multipart 上传：file=APK 文件、ids=JSON 数组字符串。
// 每个文件先落到本地临时文件再批量分发。
func (a *api) batchInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ids, tmp, err := a.receiveFile(r, "file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer os.Remove(tmp)
	results := task.InstallAPK(r.Context(), a.mgr, ids, tmp)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// batchPush 处理 multipart 上传：file=任意文件、remote=目标路径、ids=JSON 数组。
func (a *api) batchPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ids, tmp, err := a.receiveFile(r, "file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer os.Remove(tmp)
	remote := r.FormValue("remote")
	if remote == "" {
		http.Error(w, "missing remote path", http.StatusBadRequest)
		return
	}
	results := task.PushFile(r.Context(), a.mgr, ids, tmp, remote)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// receiveFile 解析 multipart（≤64MB），把上传文件存到本地临时文件并返回
// 设备 id 列表与临时文件路径。
func (a *api) receiveFile(r *http.Request, field string) ([]string, string, error) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return nil, "", err
	}
	file, _, err := r.FormFile(field)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "scrcpy-batch-*")
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", err
	}
	tmp.Close()

	var ids []string
	if raw := r.FormValue("ids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			os.Remove(tmp.Name())
			return nil, "", err
		}
	}
	if len(ids) == 0 {
		os.Remove(tmp.Name())
		return nil, "", errNoIDs
	}
	return ids, tmp.Name(), nil
}

var errNoIDs = &noIDsError{}

type noIDsError struct{}

func (*noIDsError) Error() string { return "missing ids" }
