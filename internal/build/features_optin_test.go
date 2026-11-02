package build

import (
	"strings"
	"testing"
)

// onnx / directml / effekseer は engine の option を ON にし、engine の target が無ければ configure で止まる。
func TestConfigure_OptInLibrariesTurnOnEngineOptions(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"onnx", "effekseer"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, want := range []string{
		`set(MITIRU_WITH_ONNX ON CACHE BOOL "" FORCE)`,
		`set(MITIRU_WITH_DIRECTML OFF CACHE BOOL "" FORCE)`,
		`set(MITIRU_WITH_EFFEKSEER ON CACHE BOOL "" FORCE)`,
		"if(TARGET mitiru_onnx)\nelse()",
		"if(TARGET mitiru_effekseer)\nelse()",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
	for _, unwanted := range []string{"PRIVATE mitiru_onnx", "PRIVATE mitiru_effekseer", "_mitiru_host_links mitiru_onnx"} {
		if strings.Contains(cmake, unwanted) {
			t.Errorf("the engine links these itself, but the DLL or host links %q", unwanted)
		}
	}
}

func TestConfigure_NoFeaturesForcesOptInLibrariesOff(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)
	for _, name := range []string{"MITIRU_WITH_ONNX", "MITIRU_WITH_DIRECTML", "MITIRU_WITH_EFFEKSEER", "MITIRU_WITH_CUBISM"} {
		if !strings.Contains(cmake, "set("+name+` OFF CACHE BOOL "" FORCE)`) {
			t.Errorf("%s must be forced OFF so a game that does not use it ships without it", name)
		}
	}
}

// svg と video_mf も同じ形: option を ON にし、engine の target が無ければ configure で止まる。宣言が無ければ OFF に固定する
func TestConfigure_SvgAndVideoMfTurnOnEngineOptions(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"svg", "video_mf"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, want := range []string{
		`set(MITIRU_WITH_SVG ON CACHE BOOL "" FORCE)`,
		`set(MITIRU_WITH_VIDEO_MF ON CACHE BOOL "" FORCE)`,
		`set(MITIRU_WITH_VIDEO OFF CACHE BOOL "" FORCE)`,
		"if(TARGET mitiru_svg)\nelse()",
		"if(TARGET mitiru_video_mf)\nelse()",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}

	off := generatedCMake(t, projectRoot, engineRoot)
	for _, name := range []string{"MITIRU_WITH_SVG", "MITIRU_WITH_VIDEO_MF"} {
		if !strings.Contains(off, "set("+name+` OFF CACHE BOOL "" FORCE)`) {
			t.Errorf("%s must be forced OFF without a declaration", name)
		}
	}
}
