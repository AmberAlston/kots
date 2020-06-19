package handlers

import (
	"net/http"
	"strings"

	"github.com/replicatedhq/kots/kotsadm/pkg/app"
	"github.com/replicatedhq/kots/kotsadm/pkg/logger"
	"github.com/replicatedhq/kots/kotsadm/pkg/snapshot"
)

type PingResponse struct {
	Ping                   string   `json:"ping"`
	Error                  string   `json:"error,omitempty"`
	SnapshotInProgressApps []string `json:"snapshotInProgressApps"`
}

func Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	pingResponse := PingResponse{}

	pingResponse.Ping = "pong"

	query := r.URL.Query()
	slugs := query.Get("slugs")

	if slugs != "" {
		slugsArray := strings.Split(slugs, ",")
		snapshotProgress(slugsArray, &pingResponse)
	}

	JSON(w, 200, pingResponse)
}

func snapshotProgress(slugs []string, pingResponse *PingResponse) {
	for _, slug := range slugs {
		currentApp, err := app.GetFromSlug(slug)
		if err != nil {
			logger.Error(err)
			pingResponse.Error = "failed to get app from app slug"
			return
		}

		backups, err := snapshot.ListBackupsForApp(currentApp.ID)
		if err != nil {
			logger.Error(err)
			pingResponse.Error = "failed to list backups"
			return
		}

		for _, backup := range backups {
			if backup.Status == "InProgress" {
				pingResponse.SnapshotInProgressApps = append(pingResponse.SnapshotInProgressApps, currentApp.Slug)
				return
			}
		}
	}
}
