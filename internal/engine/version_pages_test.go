package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// 最新のタグが 2 ページ目にあっても (100 件を超えても) 取りこぼさない。
func TestLatestVersionReadsEveryPage(t *testing.T) {
	var all []ghTag
	for i := 0; i < 130; i++ {
		all = append(all, ghTag{Name: fmt.Sprintf("v0.%d.0", i)})
	}
	all = append(all, ghTag{Name: "not-a-version"})
	// 一覧の並びは semver の順とは限らない。最大の v0.129.0 は 2 ページ目 (101 件目以降) にある
	all[0], all[129] = all[129], all[0]
	all[0].Name, all[129].Name = "v0.0.0", "v0.129.0"

	requested := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested++
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		lo := (page - 1) * 100
		var out []ghTag
		if lo < len(all) {
			out = all[lo:min(lo+100, len(all))]
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	old := tagsAPIBase
	tagsAPIBase = srv.URL
	defer func() { tagsAPIBase = old }()

	v, err := LatestVersionTimeout(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "0.129.0" {
		t.Errorf("latest = %s, 2 ページ目の 0.129.0 のはず", v)
	}
	if requested != 2 {
		t.Errorf("要求 %d 回: 131 件なら 2 ページで止まるはず", requested)
	}
}

func TestLatestVersionStopsOnShortPageAndReportsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	old := tagsAPIBase
	tagsAPIBase = srv.URL
	defer func() { tagsAPIBase = old }()
	if _, err := LatestVersionTimeout(5 * time.Second); err == nil {
		t.Error("API の失敗は error で返すはず")
	}
}
