package render

import (
	"encoding/json"
	"testing"
)

func TestRenderDownloadingLive_SerializesItems(t *testing.T) {
	body, err := RenderDownloadingLive([]DownloadCardView{
		{ItemID: "radarr-1", Title: "Ignored In Live Payload", Status: "downloading", Percent: 42},
		{ItemID: "sonarr-2", Status: "searching"},
	})
	if err != nil {
		t.Fatalf("RenderDownloadingLive: %v", err)
	}

	var payload struct {
		Items []struct {
			ItemID  string `json:"item_id"`
			Status  string `json:"status"`
			Percent int    `json:"percent"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].ItemID != "radarr-1" || payload.Items[0].Status != "downloading" || payload.Items[0].Percent != 42 {
		t.Errorf("items[0] = %+v, want {radarr-1 downloading 42}", payload.Items[0])
	}
	if payload.Items[1].ItemID != "sonarr-2" || payload.Items[1].Status != "searching" {
		t.Errorf("items[1] = %+v, want {sonarr-2 searching 0}", payload.Items[1])
	}
}

func TestRenderDownloadingLive_EmptyItemsSerializesEmptyArray(t *testing.T) {
	body, err := RenderDownloadingLive(nil)
	if err != nil {
		t.Fatalf("RenderDownloadingLive: %v", err)
	}
	if string(body) != `{"items":[]}` {
		t.Errorf("body = %s, want {\"items\":[]} (not null, so client-side .forEach never sees null)", body)
	}
}
