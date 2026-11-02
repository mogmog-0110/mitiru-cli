package config

import (
	"strings"
	"testing"
)

// 配布物を太らせる部品は、名前を書いたゲームだけが持つ。表の順に並び、option が付く。
func TestResolveFeatures_OptInLibraries(t *testing.T) {
	got, err := ResolveFeatures([]string{"cubism", "effekseer", "onnx", "directml"})
	if err != nil {
		t.Fatalf("ResolveFeatures: %v", err)
	}
	names := make([]string, 0, len(got))
	for _, f := range got {
		names = append(names, f.Name+"="+f.Option)
		if f.Link || f.Host {
			t.Errorf("%s is linked by the engine itself; the generated CMake must not link it again", f.Name)
		}
	}
	want := "onnx=MITIRU_WITH_ONNX directml=MITIRU_WITH_DIRECTML effekseer=MITIRU_WITH_EFFEKSEER cubism=MITIRU_WITH_CUBISM"
	if strings.Join(names, " ") != want {
		t.Errorf("features = %v, want %s", names, want)
	}
}

// 何も書かなければ 3 つとも OFF に固定される (engine の既定が変わっても、前に ON で作った cache が残らない)。
func TestFeatureOptions_OptInLibrariesAreForcedOffWhenNotDeclared(t *testing.T) {
	off := map[string]bool{}
	for _, o := range FeatureOptions(nil) {
		if !o.On {
			off[o.Name] = true
		}
	}
	for _, name := range []string{"MITIRU_WITH_ONNX", "MITIRU_WITH_DIRECTML", "MITIRU_WITH_EFFEKSEER", "MITIRU_WITH_CUBISM"} {
		if !off[name] {
			t.Errorf("%s must be forced OFF without a declaration", name)
		}
	}
}

func TestResolveFeatures_NeuralHint(t *testing.T) {
	_, err := ResolveFeatures([]string{"neural"})
	if err == nil || !strings.Contains(err.Error(), `use "onnx"`) {
		t.Fatalf("want a hint toward onnx / directml, got %v", err)
	}
}

// 動画は名前を書いたゲームだけが持つ。engine が mitiru に link する物なので、生成 CMake は option を ON にするだけ
func TestResolveFeatures_Video(t *testing.T) {
	got, err := ResolveFeatures([]string{"Video"})
	if err != nil || len(got) != 1 {
		t.Fatalf("ResolveFeatures: %v %v", got, err)
	}
	f := got[0]
	if f.Name != "video" || f.Option != "MITIRU_WITH_VIDEO" || f.Target != "mitiru_video" || f.Link || f.Host {
		t.Errorf("video feature = %+v", f)
	}
	off := false
	for _, o := range FeatureOptions(nil) {
		if o.Name == "MITIRU_WITH_VIDEO" && !o.On {
			off = true
		}
	}
	if !off {
		t.Error("MITIRU_WITH_VIDEO must be forced OFF without a declaration")
	}
	on := false
	for _, o := range FeatureOptions(got) {
		if o.Name == "MITIRU_WITH_VIDEO" && o.On {
			on = true
		}
	}
	if !on {
		t.Error("MITIRU_WITH_VIDEO must be ON with the declaration")
	}
}

func TestResolveFeatures_VideoHints(t *testing.T) {
	for _, name := range []string{"mp4", "h264", "mf"} {
		_, err := ResolveFeatures([]string{name})
		if err == nil || !strings.Contains(err.Error(), `use "video_mf"`) {
			t.Errorf("%s: want a hint toward video_mf, got %v", name, err)
		}
	}
	for _, name := range []string{"movie", "webm", "av1"} {
		if _, err := ResolveFeatures([]string{name}); err == nil || !strings.Contains(err.Error(), `use "video"`) {
			t.Errorf("%s: want a hint toward video, got %v", name, err)
		}
	}
}

// H.264 の MP4 は video とは別の feature。両方書いても並びは表の順で、option は独立に ON になる
func TestResolveFeatures_VideoMf(t *testing.T) {
	got, err := ResolveFeatures([]string{"video_mf", "Video"})
	if err != nil || len(got) != 2 {
		t.Fatalf("ResolveFeatures: %v %v", got, err)
	}
	if got[0].Name != "video" || got[1].Name != "video_mf" {
		t.Errorf("order = %s, %s; want the table order", got[0].Name, got[1].Name)
	}
	mf := got[1]
	if mf.Option != "MITIRU_WITH_VIDEO_MF" || mf.Target != "mitiru_video_mf" || mf.Link || mf.Host {
		t.Errorf("video_mf feature = %+v", mf)
	}
	state := func(sel []EngineFeature, name string) (on, found bool) {
		for _, o := range FeatureOptions(sel) {
			if o.Name == name {
				return o.On, true
			}
		}
		return false, false
	}
	if on, found := state(nil, "MITIRU_WITH_VIDEO_MF"); !found || on {
		t.Errorf("MITIRU_WITH_VIDEO_MF must be forced OFF without a declaration (found=%v on=%v)", found, on)
	}
	only, _ := ResolveFeatures([]string{"video_mf"})
	if on, _ := state(only, "MITIRU_WITH_VIDEO_MF"); !on {
		t.Error("MITIRU_WITH_VIDEO_MF must be ON with the declaration")
	}
	if on, _ := state(only, "MITIRU_WITH_VIDEO"); on {
		t.Error("video_mf alone must not turn MITIRU_WITH_VIDEO on")
	}
}

// SVG は名前を書いたゲームだけが持つ。lunasvg は engine が組むので、生成 CMake は option を ON にするだけ
func TestResolveFeatures_Svg(t *testing.T) {
	got, err := ResolveFeatures([]string{"SVG"})
	if err != nil || len(got) != 1 {
		t.Fatalf("ResolveFeatures: %v %v", got, err)
	}
	f := got[0]
	if f.Name != "svg" || f.Option != "MITIRU_WITH_SVG" || f.Target != "mitiru_svg" || f.Link || f.Host {
		t.Errorf("svg feature = %+v", f)
	}
	for _, o := range FeatureOptions(nil) {
		if o.Name == "MITIRU_WITH_SVG" && o.On {
			t.Error("MITIRU_WITH_SVG must be forced OFF without a declaration")
		}
	}
	on := false
	for _, o := range FeatureOptions(got) {
		if o.Name == "MITIRU_WITH_SVG" && o.On {
			on = true
		}
	}
	if !on {
		t.Error("MITIRU_WITH_SVG must be ON with the declaration")
	}
}
