//go:build windows

// Concilia: programa portable de conciliación bancaria.
// Sirve la interfaz embebida en 127.0.0.1 y la muestra en una ventana WebView2.
package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

//go:embed web
var webFS embed.FS

const appName = "Concilia"

var (
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	procGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")
	user32               = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
)

type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// utf16z convierte un string (que puede contener \x00 intermedios) a UTF-16 terminado en 0.
func utf16z(s string) []uint16 { return append(utf16.Encode([]rune(s)), 0) }

func messageBox(title, text string, flags uintptr) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

func downloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	d := filepath.Join(home, "Downloads")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return home
}

// saveDialog muestra el diálogo "Guardar como" de Windows. Devuelve "" si se cancela.
func saveDialog(owner uintptr, suggested, ext string) (string, error) {
	buf := make([]uint16, 1024)
	copy(buf, utf16.Encode([]rune(suggested)))
	filterTxt, titleTxt := "Libro de Excel (*.xlsx)\x00*.xlsx\x00", "Guardar conciliación"
	if ext == "json" {
		filterTxt, titleTxt = "Respaldo de Concilia (*.json)\x00*.json\x00", "Guardar respaldo del historial"
	}
	filter := utf16z(filterTxt)
	initDir := utf16z(downloadsDir())
	title := utf16z(titleTxt)
	defExt := utf16z(ext)
	ofn := openFileNameW{
		hwndOwner:       owner,
		lpstrFilter:     &filter[0],
		nFilterIndex:    1,
		lpstrFile:       &buf[0],
		nMaxFile:        uint32(len(buf)),
		lpstrInitialDir: &initDir[0],
		lpstrTitle:      &title[0],
		lpstrDefExt:     &defExt[0],
		flags:           0x00000002 | 0x00000800 | 0x00000008, // OVERWRITEPROMPT | PATHMUSTEXIST | NOCHANGEDIR
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", nil // cancelado (o error del diálogo)
	}
	return windows.UTF16ToString(buf), nil
}

type saveResult struct {
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

func listen() (net.Listener, error) {
	// Puerto fijo para que la configuración guardada (tolerancias) se conserve entre usos.
	if l, err := net.Listen("tcp", "127.0.0.1:47831"); err == nil {
		return l, nil
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

func main() {
	sub, _ := fs.Sub(webFS, "web")
	ln, err := listen()
	if err != nil {
		messageBox(appName, "No se pudo iniciar Concilia: "+err.Error(), 0x10)
		return
	}
	mux := http.NewServeMux()
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
	go http.Serve(ln, mux)

	dataDir := ""
	if base, err := os.UserCacheDir(); err == nil { // %LOCALAPPDATA%
		dataDir = filepath.Join(base, appName, "WebView2")
		_ = os.MkdirAll(dataDir, 0o755)
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataDir,
		WindowOptions: webview2.WindowOptions{
			Title:  appName + " · Conciliación bancaria",
			Width:  1320,
			Height: 880,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		messageBox(appName,
			"Concilia necesita Microsoft Edge WebView2 Runtime, que viene incluido en Windows 10 y 11 actualizados.\n\n"+
				"Instalalo gratis desde:\nhttps://go.microsoft.com/fwlink/p/?LinkId=2124703\n\nLuego volvé a abrir Concilia.", 0x30)
		return
	}
	defer w.Destroy()
	w.SetSize(1100, 700, webview2.HintMin)

	sendResult := func(res saveResult) {
		b, _ := json.Marshal(res)
		w.Eval("window.__conciliaSaved && window.__conciliaSaved(" + string(b) + ")")
	}

	// conciliaSave(nombre, base64): se responde de forma asíncrona vía window.__conciliaSaved.
	_ = w.Bind("conciliaSave", func(name, b64 string) {
		w.Dispatch(func() {
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				sendResult(saveResult{Status: "error", Error: "datos inválidos"})
				return
			}
			ext := "xlsx"
			if strings.HasSuffix(strings.ToLower(name), ".json") {
				ext = "json"
			}
			path, _ := saveDialog(uintptr(w.Window()), name, ext)
			if path == "" {
				sendResult(saveResult{Status: "cancel"})
				return
			}
			if !strings.HasSuffix(strings.ToLower(path), "."+ext) {
				path += "." + ext
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				sendResult(saveResult{Status: "error", Error: err.Error()})
				return
			}
			sendResult(saveResult{Status: "saved", Path: path})
		})
	})
	_ = w.Bind("conciliaReveal", func(path string) {
		cmd := exec.Command("explorer.exe", "/select,", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Start()
	})

	// ----- historial: un par de archivos por conciliación en %LOCALAPPDATA%\Concilia\historial
	histDir := ""
	if base, err := os.UserCacheDir(); err == nil {
		histDir = filepath.Join(base, appName, "historial")
	} else {
		histDir = filepath.Join(filepath.Dir(os.Args[0]), "historial")
	}
	validID := regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)
	writeAtomic := func(path string, data []byte) error {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	_ = w.Bind("conciliaHistList", func() ([]string, error) {
		entries, err := os.ReadDir(histDir)
		if err != nil {
			return []string{}, nil
		}
		out := []string{}
		names := []string{}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".meta.json") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			if b, err := os.ReadFile(filepath.Join(histDir, n)); err == nil {
				out = append(out, string(b))
			}
		}
		return out, nil
	})
	_ = w.Bind("conciliaHistGet", func(id string) (string, error) {
		if !validID.MatchString(id) {
			return "", fmt.Errorf("id inválido")
		}
		b, err := os.ReadFile(filepath.Join(histDir, id+".data.json"))
		if err != nil {
			return "", fmt.Errorf("no se encontró la conciliación")
		}
		return string(b), nil
	})
	_ = w.Bind("conciliaHistPut", func(id, meta, data string) error {
		if !validID.MatchString(id) {
			return fmt.Errorf("id inválido")
		}
		if err := os.MkdirAll(histDir, 0o755); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(histDir, id+".data.json"), []byte(data)); err != nil {
			return err
		}
		return writeAtomic(filepath.Join(histDir, id+".meta.json"), []byte(meta))
	})
	_ = w.Bind("conciliaHistDelete", func(id string) error {
		if !validID.MatchString(id) {
			return fmt.Errorf("id inválido")
		}
		_ = os.Remove(filepath.Join(histDir, id+".meta.json"))
		_ = os.Remove(filepath.Join(histDir, id+".data.json"))
		return nil
	})
	// Abre la página de descargas en el navegador predeterminado (solo enlaces de GitHub).
	_ = w.Bind("conciliaOpenURL", func(u string) error {
		if !strings.HasPrefix(u, "https://github.com/") {
			return fmt.Errorf("enlace no permitido")
		}
		cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Start()
	})

	w.Navigate(fmt.Sprintf("http://%s/index.html", ln.Addr().String()))
	w.Run()
}
