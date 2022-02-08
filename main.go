package main

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
)

type C = layout.Context
type D = layout.Dimensions

var colorBg = nrgb(0xF9F9F9)
var colorWhite = nrgb(0xFFFFFF)
var envHome = getenv("HOME", "/")
var envShell = getenv("SHELL", "sh")

const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"

var ansiRe = regexp.MustCompile(ansi)
var backspaceRe = regexp.MustCompile(".\b")

func main() {
	NewApp().Run()
}

type App struct {
	mx  sync.Mutex
	win *app.Window
	th  *material.Theme

	dir          string
	files        []string
	filePath     string
	fileChecksum string
	termLast     int
	termCmd      *exec.Cmd

	sidebar       widget.List
	sidebarClicks []widget.Clickable

	textEditor widget.Editor
	termEditor widget.Editor

	sidebarSplit component.Resize
	contentSplit component.Resize
}

func NewApp() *App {
	a := &App{}
	a.win = app.NewWindow(
		app.Title("nerved"),
		app.Size(dp(1200), dp(768)),
	)
	a.th = material.NewTheme(gofont.Collection())

	var err error
	a.dir, err = filepath.Abs(".")
	check(err)
	if a.dir == "/" {
		a.dir = envHome
	}
	a.files = []string{}

	a.sidebar.Axis = layout.Vertical
	a.sidebarClicks = []widget.Clickable{}
	a.loadDir(".")
	a.termEditor.Insert("$ ")
	a.termEditor.Focus()

	a.sidebarSplit.Ratio = 0.2
	a.contentSplit.Ratio = 0.5
	return a
}

func (a *App) runLoop() error {
	var ops op.Ops
	for {
		e := <-a.win.Events()
		switch e := e.(type) {
		case system.DestroyEvent:
			return e.Err
		case system.FrameEvent:
			a.Update()
			gtx := layout.NewContext(&ops, e)
			a.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) Run() {
	go func() {
		// dir & file loop
		for {
			a.loadDir(".")

			if a.filePath != "" {
				bs := []byte(a.textEditor.Text())
				sum := hex.EncodeToString(sha256.New().Sum(bs))
				if sum != a.fileChecksum {
					if err := os.WriteFile(a.filePath, bs, 0644); err != nil {
						a.termWrite("err writing: " + err.Error() + "\n")
					} else {
						a.fileChecksum = sum
					}
				} else {
					bs, err := os.ReadFile(a.filePath)
					if err == nil {
						sum = hex.EncodeToString(sha256.New().Sum(bs))
						if sum != a.fileChecksum {
							a.fileChecksum = sum
							a.textEditor.SetText(string(bs))
						}
					}
				}
			}

			time.Sleep(2 * time.Second)
		}
	}()
	go func() {
		// update & layout loop
		err := a.runLoop()
		if err != nil {
			log.Fatalln(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func (a *App) Update() {
	for i := range a.sidebarClicks {
		if a.sidebarClicks[i].Clicked() {
			if strings.HasSuffix(a.files[i], string(filepath.Separator)) {
				a.loadDir(a.files[i])
				break
			} else {
				a.loadFile(a.files[i])
				break
			}
		}
	}
	if len(a.termEditor.Events()) > 0 {
		t := a.termEditor.Text()
		selS, selE := a.termEditor.Selection()
		if selS == selE && len(t) > 0 && t[selS-1] == '\n' && selS != a.termLast {
			line := t[max(0, strings.LastIndexByte(t[:selS-1], '\n')+1) : selS-1]
			if strings.HasPrefix(line, "$") {
				a.termLast = selS
				a.termRun(line[1:])
			}
		}
	}
}

func (a *App) Layout(gtx C) {
	a.sidebarSplit.Layout(gtx, func(gtx C) D {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx C) D {
				r := cToRect(gtx.Constraints)
				defer clip.Rect(r).Push(gtx.Ops).Pop()
				paint.ColorOp{Color: colorBg}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				return D{Size: r.Max}
			}),
			layout.Expanded(func(gtx C) D {
				return (layout.Inset{Left: dp(6)}).Layout(gtx, func(gtx C) D {
					listStyle := material.List(a.th, &a.sidebar)
					return listStyle.Layout(gtx, len(a.files), func(gtx C, i int) D {
						labelStyle := material.Label(a.th, dp(16), a.files[i])
						labelStyle.Font.Variant = "Mono"
						return material.Clickable(gtx, &a.sidebarClicks[i], labelStyle.Layout)
					})
				})
			}))
	}, func(gtx C) D {
		return a.contentSplit.Layout(gtx,
			func(gtx C) D {
				editorStyle := material.Editor(a.th, &a.textEditor, "")
				editorStyle.Font.Variant = "Mono"
				return editorStyle.Layout(gtx)
			},
			func(gtx C) D {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx C) D {
						r := cToRect(gtx.Constraints)
						defer clip.Rect(r).Push(gtx.Ops).Pop()
						paint.ColorOp{Color: colorBg}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						return D{Size: r.Max}
					}),
					layout.Expanded(func(gtx C) D {
						return (layout.Inset{Left: dp(6)}).Layout(gtx, func(gtx C) D {
							editorStyle := material.Editor(a.th, &a.termEditor, "")
							editorStyle.Font.Variant = "Mono"
							return editorStyle.Layout(gtx)
						})
					}))
			},
			func(gtx C) D {
				r := rLeft(cToRect(gtx.Constraints), 6)
				return D{Size: r.Max}
			},
		)
	}, func(gtx C) D {
		r := rLeft(cToRect(gtx.Constraints), 6)
		return D{Size: r.Max}
	})
}

func (a *App) loadDir(d string) {
	var err error
	a.dir, err = filepath.Abs(filepath.Join(a.dir, d))
	check(err)

	fs, err := os.ReadDir(a.dir)
	check(err)

	a.mx.Lock()
	defer a.mx.Unlock()
	a.files = []string{".." + string(filepath.Separator)}
	for _, f := range fs {
		name := f.Name()
		if f.IsDir() {
			name = name + string(filepath.Separator)
		}
		a.files = append(a.files, name)
	}
	sort.Slice(a.files, func(i, j int) bool {
		iDir := strings.HasSuffix(a.files[i], string(filepath.Separator))
		jDir := strings.HasSuffix(a.files[j], string(filepath.Separator))
		if iDir && jDir {
			return a.files[i] < a.files[j]
		}
		return iDir
	})
	a.sidebarClicks = make([]widget.Clickable, len(a.files))
}

func (a *App) loadFile(file string) {
	path := filepath.Join(a.dir, file)
	bs, err := os.ReadFile(path)
	check(err)
	text := strings.Replace(string(bs), "\t", "  ", -1)
	sum := hex.EncodeToString(sha256.New().Sum([]byte(text)))
	a.fileChecksum = sum
	a.filePath = path
	a.textEditor.SetText(text)
}

func (a *App) termRun(commandText string) {
	if a.termCmd != nil {
		a.termCmd.Process.Signal(os.Interrupt)
	}
	cmd := exec.Command(envShell, "-l", "-c", `source "$HOME/.$(basename $SHELL)rc";`+commandText)
	cmd.Dir = a.dir
	stdout, err := cmd.StdoutPipe()
	check(err)
	stderr, err := cmd.StderrPipe()
	check(err)
	err = cmd.Start()
	if err != nil {
		a.termWrite("err: " + err.Error())
		return
	}
	a.termCmd = cmd
	go func() {
		for {
			if a.termCmd != cmd {
				return
			}
			bs := make([]byte, 1024)
			n, err := stderr.Read(bs)
			if n > 0 {
				s := ansiRe.ReplaceAllString(string(bs[:n]), "")
				s = backspaceRe.ReplaceAllString(s, "")
				a.termWrite(s)
			}
			if err == io.EOF {
				break
			}
		}
	}()
	go func() {
		for {
			if a.termCmd != cmd {
				return
			}

			bs := make([]byte, 1024)
			n, err := stdout.Read(bs)
			if n > 0 {
				s := ansiRe.ReplaceAllString(string(bs[:n]), "")
				s = backspaceRe.ReplaceAllString(s, "")
				a.termWrite(s)
			}
			if err == io.EOF {
				a.termCmd = nil
				break
			}
		}
		err := cmd.Wait()
		if err != nil {
			a.termWrite(err.Error() + "\n")
		}
		a.termWrite("$ ")
	}()
}

func (a *App) termWrite(text string) {
	a.mx.Lock()
	a.termEditor.Insert(text)
	a.mx.Unlock()
}

func dp(v float32) unit.Value {
	return unit.Dp(v)
}

func nrgb(c uint32) color.NRGBA {
	return nargb(0xff000000 | c)
}

func nargb(c uint32) color.NRGBA {
	return color.NRGBA{A: uint8(c >> 24), R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}

func cToRect(c layout.Constraints) image.Rectangle {
	return image.Rectangle{Min: c.Min, Max: c.Max}
}

func rLeft(r image.Rectangle, n int) image.Rectangle {
	return image.Rect(r.Min.X, r.Min.Y, r.Min.X+n, r.Max.Y)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getenv(key, alt string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return alt
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
