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
