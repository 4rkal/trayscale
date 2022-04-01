package main

import (
	"context"
	"embed"
	_ "embed"
	"image/color"
	"log"
	"os"
	"os/signal"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
	"github.com/DeedleFake/trayscale/tailscale"
	"github.com/getlantern/systray"
)

//go:embed assets
var assets embed.FS

const (
	prefShowWindowAtStart = "showWindowAtStart"
)

var (
	colorActive   = &color.NRGBA{0, 255, 0, 255}
	colorInactive = &color.NRGBA{255, 0, 0, 255}
)

type App struct {
	TS *tailscale.Client

	poll chan struct{}

	app fyne.App
	win fyne.Window

	status binding.Bool
}

func (a *App) pollStatus(ctx context.Context) {
	const ticklen = 5 * time.Second
	check := time.NewTicker(ticklen)

	for {
		running, err := a.TS.Status(ctx)
		if err != nil {
			log.Printf("Error: Tailscale status: %v", err)
			continue
		}
		a.status.Set(running)

		select {
		case <-ctx.Done():
			return
		case <-check.C:
		case <-a.poll:
			check.Reset(ticklen)
		}
	}
}

func (a *App) initUI(ctx context.Context) {
	a.app = app.NewWithID("trayscale")

	a.status = binding.NewBool()
	go a.pollStatus(ctx)

	statusCircle := canvas.NewCircle(colorActive)
	a.status.AddListener(binding.NewDataListener(func() {
		defer statusCircle.Refresh()
		running, _ := a.status.Get()
		if running {
			statusCircle.FillColor = colorActive
			return
		}
		statusCircle.FillColor = colorInactive
	}))

	a.win = a.app.NewWindow("Trayscale")
	a.win.SetContent(
		container.NewCenter(
			container.NewVBox(
				container.NewCenter(
					container.NewHBox(
						widget.NewRichTextFromMarkdown(`# Trayscale`),
						container.NewCenter(container.NewGridWrap(fyne.NewSize(32, 32), statusCircle)),
					),
				),
				widget.NewCheckWithData(
					"Show Window at Start",
					binding.BindPreferenceBool(prefShowWindowAtStart, a.app.Preferences()),
				),
				widget.NewButton("Quit", func() { a.Quit() }),
			),
		),
	)
	a.win.SetFixedSize(true)
	a.win.SetCloseIntercept(func() { a.win.Hide() })

	if a.app.Preferences().Bool(prefShowWindowAtStart) {
		a.win.Show()
	}
}

func (a *App) updateIcon() {
	icon := "assets/icon-active.png"
	active, _ := a.status.Get()
	if !active {
		icon = "assets/icon-inactive.png"
	}

	data, _ := assets.ReadFile(icon)
	systray.SetIcon(data)
}

func (a *App) initTray(ctx context.Context) {
	a.status.AddListener(binding.NewDataListener(a.updateIcon))

	newTrayItem(ctx, "Show", func() { a.win.Show() })

	systray.AddSeparator()

	start := newTrayItem(ctx, "Start", func() {
		err := a.TS.Start(ctx)
		if err != nil {
			log.Printf("Error: start tailscale: %v", err)
		}
		a.poll <- struct{}{}
	})
	a.status.AddListener(binding.NewDataListener(func() {
		active, _ := a.status.Get()
		if active {
			start.Disable()
			return
		}
		start.Enable()
	}))

	stop := newTrayItem(ctx, "Stop", func() {
		err := a.TS.Stop(ctx)
		if err != nil {
			log.Printf("Error: stop tailscale: %v", err)
		}
		a.poll <- struct{}{}
	})
	a.status.AddListener(binding.NewDataListener(func() {
		active, _ := a.status.Get()
		if !active {
			stop.Disable()
			return
		}
		stop.Enable()
	}))

	systray.AddSeparator()

	newTrayItem(ctx, "Exit", func() {
		a.Quit()
	})
}

func (a *App) Quit() {
	a.app.Quit()
	systray.Quit()
}

func (a *App) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.poll = make(chan struct{}, 1)

	a.initUI(ctx)
	a.initTray(ctx)

	go systray.Run(
		func() {
			go func() {
				<-ctx.Done()
				systray.Quit()
			}()
		},
		nil,
	)

	go func() {
		<-ctx.Done()
		a.app.Quit()
	}()

	a.app.Run()
}

func newTrayItem(ctx context.Context, label string, onClick func()) *systray.MenuItem {
	item := systray.AddMenuItem(label, "")
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-item.ClickedCh:
				onClick()
			}
		}
	}()
	return item
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ts := tailscale.Client{
		Command: "tailscale",
	}

	a := App{
		TS: &ts,
	}
	a.Run(ctx)
}
