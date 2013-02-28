package main

import (
	"fmt"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/keybind"
	"github.com/BurntSushi/xgbutil/mousebind"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil/xwindow"
	"code.google.com/p/jamslam-freetype-go/freetype/truetype"
	"image"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

type Font struct {
	Color xgraphics.BGRA
	Size float64
	TrueType *truetype.Font
}

func loadFont(name string) *Font {
	fontReader, err := os.Open(name)
	if err != nil {
		log.Fatal(err)
	}

	font, err := xgraphics.ParseFont(fontReader)
	if err != nil {
		log.Fatal(err)
	}
	return &Font{Size: 12.0, TrueType: font}
}

type Item struct {
	Text <-chan string
	displayed string
	offset int
	*Font
	image *xgraphics.Image
	window *xwindow.Window
}

func (i *Item) update(text string) {
	if text == i.displayed {
		return
	} else if i.displayed == "" {
		i.displayed = text
	}
	width, height := xgraphics.Extents(i.TrueType, i.Size, i.displayed)

	sub := i.image.SubImage(image.Rect(10, i.offset, 10 + width, height + i.offset))
	sub.ForExp(func(x, y int) (r, b, g, a uint8) {
		return 0x00, 0x00, 0x00, 0x00
	})
	sub.Text(10, i.offset, i.Color, i.Size, i.TrueType, text)
	sub.XDraw()
	sub.XExpPaint(i.window.Id, 10, i.offset)
	i.displayed = text
}

func batteryItem(font *Font, offset int, image *xgraphics.Image, window *xwindow.Window) *Item {
	channel := make(chan string, 8)
	go func() {
		file, err := os.Open("/sys/class/power_supply/BAT0/uevent")
		if err != nil {
			return
		}

		var power_supply_charge_now,
			power_supply_current_now,
			power_supply_charge_full uint64
		var power_supply_status string
		for time := range time.NewTicker(time.Second).C {
			_ = time

			text, err := ioutil.ReadAll(file)
			if err != nil {
				log.Fatal(err)
			}
			_, err = file.Seek(0, 0)
			if err != nil {
				log.Fatal(err)
			}

			begin, state, key := 0, 0, ""
			for end, c := range text {
				if state == 0 && c == '=' {
					begin, key, state = end+1, string(text[begin:end]), 1
				} else if state == 1 && c == '\n' {
					value := string(text[begin:end])
					switch key {
					case "POWER_SUPPLY_CHARGE_NOW":
						fmt.Sscan(value, &power_supply_charge_now)
					case "POWER_SUPPLY_CURRENT_NOW":
						fmt.Sscan(value, &power_supply_current_now)
					case "POWER_SUPPLY_CHARGE_FULL":
						fmt.Sscan(value, &power_supply_charge_full)
					case "POWER_SUPPLY_STATUS":
						power_supply_status = value
					}

					begin, state = end+1, 0
				}
			}

			if power_supply_current_now == 0 {
				charge := (100*power_supply_charge_now)/power_supply_charge_full
				channel <- fmt.Sprintf("Battery: %v%% ??:??:?? %v",
					charge,
					power_supply_status)
			} else {
				charge := (100*power_supply_charge_now)/power_supply_charge_full
				if power_supply_status == "Charging" {
					power_supply_charge_now =
						power_supply_charge_full - power_supply_charge_now
				}
				seconds := (3600*power_supply_charge_now)/power_supply_current_now
				channel <- fmt.Sprintf("Battery: %v%% %v:%02v:%02v %v",
					charge,
					seconds/3600,
					(seconds%3600)/60,
					(seconds%3600)%60,
					power_supply_status)
			}
		}
	}()
	return &Item{Text: channel, Font: font, offset: offset, image: image, window: window}
}

func cpuItem(font *Font, offset int, image *xgraphics.Image, window *xwindow.Window) *Item {
	channel := make(chan string, 8)
	go func() {
		file, err := os.Open("/proc/stat")
		if err != nil {
			log.Fatal(err)
		}

		var user_mode, user_mode_nice, system_mode, idel [2]uint64
		for time := range time.NewTicker(time.Second).C {
			_ = time

			text, err := ioutil.ReadAll(file)
			if err != nil {
				log.Fatal(err)
			}
			_, err = file.Seek(0, 0)
			if err != nil {
				log.Fatal(err)
			}

			fields := strings.Fields(string(text))
			fmt.Sscan(fields[1], &user_mode[0])
			fmt.Sscan(fields[2], &user_mode_nice[0])
			fmt.Sscan(fields[3], &system_mode[0])
			fmt.Sscan(fields[4], &idel[0])

			channel <- fmt.Sprintf("CPU: %3v%% user %3v%% system %3v%% idel",
					user_mode[0] - user_mode[1] + user_mode_nice[0] - user_mode_nice[1],
					system_mode[0] - system_mode[1],
					idel[0] - idel[1])

			user_mode[1], user_mode_nice[1], system_mode[1], idel[1] =
				user_mode[0], user_mode_nice[0], system_mode[0], idel[0]
		}
	}()
	return &Item{Text: channel, Font: font, offset: offset, image: image, window: window}
}

func memoryItem(font *Font, offset int, image *xgraphics.Image, window *xwindow.Window) *Item {
	channel := make(chan string, 8)
	go func() {
		file, err := os.Open("/proc/meminfo")
		if err != nil {
			log.Fatal(err)
		}

		for time := range time.NewTicker(time.Second).C {
			_ = time

			text, err := ioutil.ReadAll(file)
			if err != nil {
				log.Fatal(err)
			}
			_, err = file.Seek(0, 0)
			if err != nil {
				log.Fatal(err)
			}

			fields := strings.Fields(string(text))
			var mem_total, mem_free uint64
			fmt.Sscan(fields[1], &mem_total)
			fmt.Sscan(fields[4], &mem_free)

			channel <- fmt.Sprintf("Memory: %3v%%", (100 * (mem_total - mem_free)) / mem_total)
		}
	}()
	return &Item{Text: channel, Font: font, offset: offset, image: image, window: window}
}

func main() {
	runtime.GOMAXPROCS(64)

	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal(err)
	}

	keybind.Initialize(X)

	font := loadFont("/usr/share/fonts/truetype/freefont/FreeMono.ttf")
	font.Color = xgraphics.BGRA{B: 0x00, G: 0xff, R: 0x00, A: 0xff}
	font.Size = 12.0

	ximage := xgraphics.New(X, image.Rect(0, 0, 300, 300))
	ximage.CreatePixmap()
	window, obscured := makeWindow(ximage)

	battery := batteryItem(font, 10, ximage, window)
	cpu := cpuItem(font, 30, ximage, window)
	memory := memoryItem(font, 50, ximage, window)
	before, after, quit := xevent.MainPing(X)
	loop: for {
		select {
		case <-before:
			<-after
		case <-quit:
			break loop
		case text := <-battery.Text:
			if *obscured  {
				continue loop
			}
			battery.update(text)
		case text := <-cpu.Text:
			if *obscured  {
				continue loop
			}
			cpu.update(text)
		case text := <-memory.Text:
			if *obscured  {
				continue loop
			}
			memory.update(text)
		}
	}
}

func makeWindow(ximage *xgraphics.Image) (*xwindow.Window, *bool) {
	w, h := ximage.Rect.Dx(), ximage.Rect.Dy()

	window, err := xwindow.Generate(ximage.X)
	if err != nil {
		xgbutil.Logger.Printf("Could not generate new window id: %s", err)
		return nil, nil
	}

	window.Create(ximage.X.RootWin(), 0, 0, w, h, xproto.CwBackPixel, 0x00000000)
	window.Listen(xproto.EventMaskExposure,
		xproto.EventMaskKeyPress,
		xproto.EventMaskStructureNotify,
		xproto.EventMaskVisibilityChange)

	window.WMGracefulClose(func(w *xwindow.Window) {
		xevent.Detach(w.X, w.Id)
		keybind.Detach(w.X, w.Id)
		mousebind.Detach(w.X, w.Id)
		w.Destroy()
		xevent.Quit(w.X)
	})

	err = icccm.WmStateSet(ximage.X, window.Id, &icccm.WmState{
		State: icccm.StateNormal,
	})
	if err != nil {
		xgbutil.Logger.Printf("Could not set WM_STATE: %s", err)
	}

	err = ewmh.WmNameSet(ximage.X, window.Id, "Computer System Monitor")
	if err != nil {
		xgbutil.Logger.Printf("Could not set _NET_WM_NAME: %s", err)
	}

		err = keybind.KeyPressFun(
		func(X *xgbutil.XUtil, ev xevent.KeyPressEvent) {
			err := ewmh.WmStateReq(ximage.X, window.Id, ewmh.StateToggle,
				"_NET_WM_STATE_FULLSCREEN")
			if err != nil {
				log.Fatal(err)
			}
		}).Connect(ximage.X, window.Id, "f", false)
	if err != nil {
		log.Fatal(err)
	}

	xevent.ExposeFun(
		func(xu *xgbutil.XUtil, event xevent.ExposeEvent) {
			ximage.XExpPaint(window.Id, 0, 0)
		}).Connect(ximage.X, window.Id)

	obscured := false
	xevent.VisibilityNotifyFun(
		func(xu *xgbutil.XUtil, event xevent.VisibilityNotifyEvent) {
			obscured = event.State == xproto.VisibilityFullyObscured
		}).Connect(ximage.X, window.Id)

	window.Map()

	return window, &obscured
}
