package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// renderFeaturesJSON は engine が 3D 描画の機能の表から書き出した JSON (engine の root からの相対)。
// C++ のヘッダーの字面ではなく、この JSON を読む。engine の tools/gen_render_features_doc.py が作り、
// BuildFlag を組み込む [engine] features の名前も engine が持つ。
const renderFeaturesJSON = "include/mitiru/render/render_features.json"

// renderFeaturesSchema はこの CLI が読める JSON の版。engine が上げたら、CLI も読み方を直す。
const renderFeaturesSchema = 1

// featureRowInfo は engine の機能の表の 1 行のうち、宣言の検査に要る部分。
type featureRowInfo struct {
	Key           string  `json:"key"`
	Declarable    bool    `json:"declarable"`
	Build         string  `json:"build"`          // engine の BuildFlag の名前 (小文字)
	EngineFeature *string `json:"engine_feature"` // Build が none 以外のとき必ずある。空は mitiru.toml からは足せない部品
}

type featureTableFile struct {
	Schema int              `json:"schema"`
	Rows   []featureRowInfo `json:"rows"`
}

// readFeatureTable は engine の render_features.json から表の行を読む。JSON が無い engine は os.ErrNotExist。
func readFeatureTable(engineRoot string) ([]featureRowInfo, error) {
	body, err := os.ReadFile(filepath.Join(engineRoot, filepath.FromSlash(renderFeaturesJSON)))
	if err != nil {
		return nil, err
	}
	var file featureTableFile
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, fmt.Errorf("%s: JSON を読めません: %w", renderFeaturesJSON, err)
	}
	if file.Schema != renderFeaturesSchema {
		return nil, fmt.Errorf("%s: schema %d はこの mitiru-cli が読める %d と違います。engine と mitiru-cli の版をそろえてください",
			renderFeaturesJSON, file.Schema, renderFeaturesSchema)
	}
	if len(file.Rows) == 0 {
		return nil, fmt.Errorf("%s: 表の行がありません", renderFeaturesJSON)
	}
	for i, row := range file.Rows {
		if err := row.validate(); err != nil {
			return nil, fmt.Errorf("%s: %d 行目: %w", renderFeaturesJSON, i+1, err)
		}
	}
	return file.Rows, nil
}

func (r featureRowInfo) validate() error {
	switch {
	case r.Key == "":
		return fmt.Errorf("key がありません")
	case r.Build == "":
		return fmt.Errorf("%s: build がありません", r.Key)
	case r.Build != buildNone && r.EngineFeature == nil:
		return fmt.Errorf("%s: build %q の engine_feature がありません。engine の render_features.json を作り直してください", r.Key, r.Build)
	}
	return nil
}

// buildNone は外部の部品を要らない行の build の値。
const buildNone = "none"
