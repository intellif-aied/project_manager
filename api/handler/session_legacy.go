package handler

import "net/http"

// LegacySessionBatchUploadDisabled keeps the old CLI endpoint explicit while
// preventing legacy uploads from bypassing the incremental sync ledger.
func LegacySessionBatchUploadDisabled(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusUpgradeRequired, map[string]string{
		"code":    "CLI_UPGRADE_REQUIRED",
		"message": "当前 Aida 版本已停止支持，请更新 Aida CLI 后重新上传 Session",
	})
}
