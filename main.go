package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
)

// 排盘记录持久化：落盘为 JSON 文件（data/records.json），不依赖浏览器本地存储。
const (
	addr        = ":8080"
	dataDir     = "data"
	recordsFile = "data/records.json"
)

var recordMu sync.Mutex // 串行化记录文件的读写，避免并发写坏

// Record 一条排盘记录，字段与前端一致
type Record struct {
	ID       string `json:"id"`
	SavedAt  int64  `json:"savedAt"`  // 排盘（保存）时刻，毫秒时间戳
	Args     []int  `json:"args"`     // 盘之时刻：年 月 日 时 分 秒
	When     string `json:"when"`     // 盘之时刻显示文本
	Question string `json:"question"` // 所测何事
	Analysis string `json:"analysis"` // 简要分析
	JuTag    string `json:"juTag"`    // 局数摘要
}

func readRecords() ([]Record, error) {
	b, err := os.ReadFile(recordsFile)
	if err != nil {
		if os.IsNotExist(err) { // 首次运行时文件还不存在
			return []Record{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return []Record{}, nil
	}
	var rs []Record
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, err
	}
	return rs, nil
}

func writeRecords(rs []Record) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	tmp := recordsFile + ".tmp" // 先写临时文件再改名，避免中途失败写坏原文件
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, recordsFile)
}

func jsonResp(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// /api/records
//   GET  列出全部记录
//   POST 追加一条（新记录排在最前）
func handleRecords(w http.ResponseWriter, r *http.Request) {
	recordMu.Lock()
	defer recordMu.Unlock()

	switch r.Method {
	case http.MethodGet:
		rs, err := readRecords()
		if err != nil {
			jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, http.StatusOK, map[string]interface{}{"records": rs})

	case http.MethodPost:
		var rec Record
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
			return
		}
		if rec.ID == "" {
			jsonResp(w, http.StatusBadRequest, map[string]string{"error": "缺少 id"})
			return
		}
		rs, err := readRecords()
		if err != nil {
			jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		rs = append([]Record{rec}, rs...)
		if err := writeRecords(rs); err != nil {
			jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		jsonResp(w, http.StatusOK, map[string]interface{}{"ok": true, "total": len(rs)})

	default:
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET / POST"})
	}
}

// /api/records/delete  POST {"id":"..."} 删除一条记录
func handleRecordDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": "缺少 id"})
		return
	}

	recordMu.Lock()
	defer recordMu.Unlock()

	rs, err := readRecords()
	if err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	kept := rs[:0] // 原地过滤
	found := false
	for _, v := range rs {
		if v.ID == body.ID {
			found = true
			continue
		}
		kept = append(kept, v)
	}
	if !found {
		jsonResp(w, http.StatusNotFound, map[string]string{"error": "记录不存在"})
		return
	}
	if err := writeRecords(kept); err != nil {
		jsonResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, http.StatusOK, map[string]interface{}{"ok": true, "total": len(kept)})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/records", handleRecords)
	mux.HandleFunc("/api/records/delete", handleRecordDelete)
	mux.Handle("/", http.FileServer(http.Dir("static")))

	log.Printf("易经研习服务已启动: http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
