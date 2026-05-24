package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/spf13/cobra"
)

const mockCEFStateTemplate = `(function(global){
'use strict';
var _initialState = %s;
var mitiru = global.mitiru = global.mitiru || {};
var _state = mitiru._state = mitiru._state || {};
var _stateListeners = Object.create(null);
var _eventListeners = Object.create(null);
var _retained = Object.create(null);
Object.keys(_initialState).forEach(function(k){ _retained[k] = _initialState[k]; });
_state._onChange = function(key, value){
  _retained[key] = value;
  var arr = _stateListeners[key];
  if(!arr) return;
  var copy = arr.slice();
  for(var i=0;i<copy.length;i++){
    try{ copy[i](value); }catch(e){ console.error('[mitiru.mock._onChange]',e); }
  }
};
_state._onEvent = function(name, payload){
  var arr = _eventListeners[name];
  if(!arr) return;
  var copy = arr.slice();
  for(var i=0;i<copy.length;i++){
    try{ copy[i](payload); }catch(e){ console.error('[mitiru.mock._onEvent]',e); }
  }
};
mitiru.onStateChange = function(key, fn){
  if(typeof key!=='string'||typeof fn!=='function')
    throw new Error('mitiru.onStateChange: (string, function) required');
  if(!_stateListeners[key]){ _stateListeners[key]=[]; }
  _stateListeners[key].push(fn);
  if(Object.prototype.hasOwnProperty.call(_retained,key)){
    try{ fn(_retained[key]); }catch(e){ console.error('[mitiru.mock] initial fire threw:',e); }
  }
  return function(){ mitiru.offStateChange(key,fn); };
};
mitiru.offStateChange = function(key, fn){
  var arr = _stateListeners[key];
  if(!arr) return;
  var i = arr.indexOf(fn);
  if(i>=0) arr.splice(i,1);
};
mitiru.on = function(name, fn){
  if(typeof name!=='string'||typeof fn!=='function')
    throw new Error('mitiru.on: (string, function) required');
  if(!_eventListeners[name]){ _eventListeners[name]=[]; }
  _eventListeners[name].push(fn);
  return function(){ mitiru.off(name,fn); };
};
mitiru.off = function(name, fn){
  var arr = _eventListeners[name];
  if(!arr) return;
  var i = arr.indexOf(fn);
  if(i>=0) arr.splice(i,1);
};
mitiru.getState = function(key){ return _retained[key]; };
mitiru.dispatch = function(action, payload){
  if(typeof action!=='string')
    return Promise.reject(new Error('mitiru.dispatch: action must be string'));
  console.log('[mitiru.mock.dispatch]', action, payload);
  return Promise.resolve(null);
};
})(typeof window!=='undefined'?window:globalThis);
`

const cefStateURLPath = "/mitiru_runtime/mitiru_cef_state.js"

func newUICommand() *cobra.Command {
	var stateFile string
	var port int

	cmd := &cobra.Command{
		Use:   "ui [scene.html]",
		Short: "Preview HTML/CSS game UI in the browser instantly, no build needed",
		Long: `Start a local HTTP server serving the project's assets/ directory and
open the scene in your default browser with a mock window.mitiru bridge.
The mock implements the full onStateChange / offStateChange / on / off /
getState / dispatch API so mitiru_bind.js renders against mock state without
running the C++ engine.

  mitiru ui                         serve assets/scene.html on :8137
  mitiru ui assets/hud.html         serve a specific scene
  mitiru ui --state mock.json       seed retained state from a JSON file
  mitiru ui --port 9000             use a custom port

The state JSON should be a flat object whose keys match the state keys your
scene subscribes to, e.g. {"view.hp": 80, "view.score": 1200}.

Press Ctrl+C to stop the server.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(args, stateFile, port)
		},
	}

	cmd.Flags().StringVar(&stateFile, "state", "", "JSON file with initial mock state values")
	cmd.Flags().IntVar(&port, "port", 8137, "local port to serve on")
	return cmd
}

func runUI(args []string, stateFile string, port int) error {
	// Resolve project root and assets dir.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	_, projectRoot, err := config.FindManifest(cwd)
	if err != nil {
		return fmt.Errorf("not inside a mitiru project: %w", err)
	}

	assetsDir := filepath.Join(projectRoot, "assets")
	if st, statErr := os.Stat(assetsDir); statErr != nil || !st.IsDir() {
		return fmt.Errorf("assets/ not found at %s — run 'mitiru build' once to populate it", assetsDir)
	}

	// Determine scene path (URL-relative to assets/).
	sceneURL := "/scene.html"
	if len(args) == 1 {
		rel, relErr := filepath.Rel(assetsDir, filepath.Join(projectRoot, filepath.FromSlash(args[0])))
		if relErr != nil {
			// Treat arg as already relative to assets/.
			rel = filepath.ToSlash(args[0])
		}
		sceneURL = "/" + filepath.ToSlash(rel)
	}

	// Load initial mock state.
	initialState := map[string]interface{}{}
	if stateFile != "" {
		raw, readErr := os.ReadFile(stateFile)
		if readErr != nil {
			return fmt.Errorf("read --state %s: %w", stateFile, readErr)
		}
		if jsonErr := json.Unmarshal(raw, &initialState); jsonErr != nil {
			return fmt.Errorf("parse --state %s: %w", stateFile, jsonErr)
		}
	}

	stateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	mockJS := fmt.Sprintf(mockCEFStateTemplate, string(stateJSON))

	// Build HTTP handler: intercept mitiru_cef_state.js, serve rest from assets/.
	fileServer := http.FileServer(http.Dir(assetsDir))
	mux := http.NewServeMux()
	mux.HandleFunc(cefStateURLPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(mockJS))
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block the real mitiru_cef_state.js from disk (already handled above,
		// but guard in case path casing differs).
		if strings.EqualFold(r.URL.Path, cefStateURLPath) {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = w.Write([]byte(mockJS))
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := fmt.Sprintf("http://%s%s", addr, sceneURL)

	server := &http.Server{Addr: addr, Handler: mux}

	// Start listener before opening browser so the page is ready.
	fmt.Printf("mitiru ui  →  %s\n", url)
	fmt.Println("Ctrl+C to stop.")

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	// Open browser (Windows).
	_ = exec.Command("cmd", "/c", "start", url).Start()

	// Block until Ctrl+C or server error.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		fmt.Println("\nStopping.")
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	}
}
