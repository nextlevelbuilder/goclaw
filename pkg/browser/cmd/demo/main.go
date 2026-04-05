// GoClaw Browser LiveView Demo
//
// Interactive remote browser: screencast + mouse + keyboard via native CDP.
// Coordinate mapping: client sends screencast-image coords, server maps to CSS viewport.
//
// Run:  go run ./pkg/browser/cmd/demo/ [url]
// Open: http://localhost:9222
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

var (
	latestFrame []byte
	frameMu     sync.RWMutex

	// Actual browser viewport size (CSS pixels) — needed for coordinate mapping
	viewportW float64 = 1280
	viewportH float64 = 720

	// Screencast image size (may differ from viewport due to device pixel ratio)
	screencastW float64 = 1280
	screencastH float64 = 720

	wsClients   = make(map[*websocket.Conn]bool)
	wsClientsMu sync.Mutex
)

type InputEvent struct {
	Type   string  `json:"type"`
	X      float64 `json:"x"` // in screencast image coordinates
	Y      float64 `json:"y"`
	Button int     `json:"button"`
	DeltaX float64 `json:"deltaX"`
	DeltaY float64 `json:"deltaY"`
	Key    string  `json:"key"`
	Code   string  `json:"code"`
	Shift  bool    `json:"shift"`
	Ctrl   bool    `json:"ctrl"`
	Alt    bool    `json:"alt"`
	Meta   bool    `json:"meta"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	ctx := context.Background()
	targetURL := "https://news.ycombinator.com"
	if len(os.Args) > 1 {
		targetURL = os.Args[1]
	}

	port := "9222"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	fmt.Println("=== GoClaw LiveView ===")

	mgr := browser.New(
		browser.WithHeadless(false),
		browser.WithIdleTimeout(0),
	)
	defer mgr.Close()

	if err := mgr.Start(ctx); err != nil {
		log.Fatal(err)
	}

	tab, err := mgr.OpenTab(ctx, targetURL)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("[OK] %s\n", tab.URL)

	// Set a known viewport size so coordinate mapping is deterministic
	if err := mgr.Emulate(ctx, tab.TargetID, browser.EmulateOpts{
		Width:  1280,
		Height: 720,
		Scale:  1,
	}); err != nil {
		log.Printf("emulate warning: %v", err)
	}
	viewportW, viewportH = 1280, 720

	// Get actual viewport to be safe
	go func() {
		time.Sleep(1 * time.Second)
		res, err := mgr.Evaluate(ctx, tab.TargetID, `() => JSON.stringify({w:window.innerWidth, h:window.innerHeight})`)
		if err == nil {
			var dims struct {
				W float64 `json:"w"`
				H float64 `json:"h"`
			}
			if json.Unmarshal([]byte(res), &dims) == nil && dims.W > 0 {
				viewportW = dims.W
				viewportH = dims.H
				fmt.Printf("[OK] Viewport: %.0fx%.0f\n", viewportW, viewportH)
			}
		}
	}()

	ch, err := mgr.StartScreencast(ctx, tab.TargetID, 15, 75)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for frame := range ch {
			frameMu.Lock()
			latestFrame = frame.Data
			// Track screencast frame dimensions for coordinate mapping
			if frame.Metadata.DeviceWidth > 0 {
				screencastW = frame.Metadata.DeviceWidth
				screencastH = frame.Metadata.DeviceHeight
			}
			frameMu.Unlock()

			wsClientsMu.Lock()
			for c := range wsClients {
				_ = c.WriteMessage(websocket.BinaryMessage, frame.Data)
			}
			wsClientsMu.Unlock()
		}
	}()
	fmt.Println("[OK] Screencast streaming")

	// --- Input dispatch using native CDP ---
	dispatch := func(ev InputEvent) {
		// Map screencast image coordinates → CSS viewport coordinates
		// screencast image may be scaled (e.g. 1280 image for 1280 viewport = 1:1)
		scaleX := viewportW / screencastW
		scaleY := viewportH / screencastH
		cssX := ev.X * scaleX
		cssY := ev.Y * scaleY
		mod := modifiers(ev)

		switch ev.Type {
		case "mousemove":
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mouseMoved", cssX, cssY, "none", 0)

		case "mousedown":
			btn := cdpButton(ev.Button)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mousePressed", cssX, cssY, btn, 1)

		case "mouseup":
			btn := cdpButton(ev.Button)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mouseReleased", cssX, cssY, btn, 1)

		case "click":
			btn := cdpButton(ev.Button)
			// Native CDP click = mousePressed + mouseReleased at same position
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mousePressed", cssX, cssY, btn, 1)
			time.Sleep(30 * time.Millisecond)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mouseReleased", cssX, cssY, btn, 1)

		case "dblclick":
			btn := cdpButton(ev.Button)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mousePressed", cssX, cssY, btn, 1)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mouseReleased", cssX, cssY, btn, 1)
			time.Sleep(10 * time.Millisecond)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mousePressed", cssX, cssY, btn, 2)
			_ = mgr.DispatchMouseEvent(ctx, tab.TargetID, "mouseReleased", cssX, cssY, btn, 2)

		case "scroll":
			_, _ = mgr.Evaluate(ctx, tab.TargetID,
				fmt.Sprintf(`() => window.scrollBy(%f, %f)`, ev.DeltaX, ev.DeltaY))

		case "keydown":
			text := ""
			if len(ev.Key) == 1 {
				text = ev.Key
			}
			vk := keyToVKCode(ev.Key, ev.Code)
			_ = mgr.DispatchKeyEvent(ctx, tab.TargetID, "keyDown", ev.Key, ev.Code, "", mod, vk)
			// char event needed for actual text input
			if text != "" {
				_ = mgr.DispatchKeyEvent(ctx, tab.TargetID, "char", ev.Key, ev.Code, text, mod, vk)
			}

		case "keyup":
			vk := keyToVKCode(ev.Key, ev.Code)
			_ = mgr.DispatchKeyEvent(ctx, tab.TargetID, "keyUp", ev.Key, ev.Code, "", mod, vk)
		}
	}

	// --- HTTP routes ---

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		html := strings.Replace(strings.Replace(liveViewHTML, "__TARGET__", targetURL, 1), "__PORT__", port, 1)
		fmt.Fprint(w, html)
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		wsClientsMu.Lock()
		wsClients[conn] = true
		wsClientsMu.Unlock()
		defer func() {
			wsClientsMu.Lock()
			delete(wsClients, conn)
			wsClientsMu.Unlock()
		}()

		frameMu.RLock()
		if len(latestFrame) > 0 {
			_ = conn.WriteMessage(websocket.BinaryMessage, latestFrame)
		}
		frameMu.RUnlock()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var ev InputEvent
			if json.Unmarshal(msg, &ev) == nil {
				go dispatch(ev)
			}
		}
	})

	http.HandleFunc("/navigate", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Query().Get("url")
		if url == "" {
			http.Error(w, "url required", 400)
			return
		}
		if err := mgr.Navigate(ctx, tab.TargetID, url); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write([]byte("ok"))
	})

	fmt.Printf("\n  >>> http://localhost:%s <<<\n\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func cdpButton(jsButton int) string {
	switch jsButton {
	case 0:
		return "left"
	case 1:
		return "middle"
	case 2:
		return "right"
	default:
		return "none"
	}
}

func modifiers(ev InputEvent) int {
	m := 0
	if ev.Alt {
		m |= 1
	}
	if ev.Ctrl {
		m |= 2
	}
	if ev.Meta {
		m |= 4
	}
	if ev.Shift {
		m |= 8
	}
	return m
}

// keyToVKCode maps JS key names to Windows virtual key codes.
// CDP Input.dispatchKeyEvent requires windowsVirtualKeyCode for special keys to work.
func keyToVKCode(key, code string) int {
	switch key {
	case "Backspace":
		return 8
	case "Tab":
		return 9
	case "Enter":
		return 13
	case "Shift":
		return 16
	case "Control":
		return 17
	case "Alt":
		return 18
	case "Pause":
		return 19
	case "CapsLock":
		return 20
	case "Escape":
		return 27
	case " ":
		return 32
	case "PageUp":
		return 33
	case "PageDown":
		return 34
	case "End":
		return 35
	case "Home":
		return 36
	case "ArrowLeft":
		return 37
	case "ArrowUp":
		return 38
	case "ArrowRight":
		return 39
	case "ArrowDown":
		return 40
	case "Insert":
		return 45
	case "Delete":
		return 46
	case "Meta":
		return 91
	case "F1":
		return 112
	case "F2":
		return 113
	case "F3":
		return 114
	case "F4":
		return 115
	case "F5":
		return 116
	case "F6":
		return 117
	case "F7":
		return 118
	case "F8":
		return 119
	case "F9":
		return 120
	case "F10":
		return 121
	case "F11":
		return 122
	case "F12":
		return 123
	case ";":
		return 186
	case "=":
		return 187
	case ",":
		return 188
	case "-":
		return 189
	case ".":
		return 190
	case "/":
		return 191
	case "`":
		return 192
	case "[":
		return 219
	case "\\":
		return 220
	case "]":
		return 221
	case "'":
		return 222
	}
	// For printable single-character keys, use char code
	if len(key) == 1 {
		ch := int(key[0])
		// a-z → 65-90 (same VK as uppercase)
		if ch >= 'a' && ch <= 'z' {
			return ch - 32
		}
		// A-Z → 65-90
		if ch >= 'A' && ch <= 'Z' {
			return ch
		}
		// 0-9 → 48-57
		if ch >= '0' && ch <= '9' {
			return ch
		}
		return ch
	}
	return 0
}

const liveViewHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>GoClaw LiveView</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0a0a0a;overflow:hidden;font-family:-apple-system,system-ui,sans-serif}
.bar{height:40px;background:#1a1a1a;border-bottom:1px solid #333;display:flex;align-items:center;padding:0 12px;gap:8px}
.bar .logo{color:#0f0;font-size:13px;font-weight:700;font-family:monospace}
.bar input{flex:1;height:28px;background:#111;border:1px solid #444;border-radius:4px;color:#ccc;font-size:13px;padding:0 10px;outline:none}
.bar input:focus{border-color:#0f0}
.bar button{height:28px;padding:0 14px;background:#222;border:1px solid #444;border-radius:4px;color:#aaa;font-size:12px;cursor:pointer}
.bar button:hover{background:#333;color:#fff}
.bar .st{font-size:11px;color:#666;font-family:monospace}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:4px}
.on{background:#0f0}.off{background:#f00}
.wrap{width:100vw;height:calc(100vh - 40px);display:flex;align-items:center;justify-content:center;background:#000;position:relative}
canvas{cursor:default;max-width:100%;max-height:100%}
.info{position:absolute;bottom:8px;right:12px;color:#444;font-size:10px;font-family:monospace}
</style>
</head>
<body>
<div class="bar">
  <span class="logo">GoClaw</span>
  <input id="url" value="__TARGET__" onkeydown="if(event.key==='Enter'){nav(this.value)}">
  <button onclick="nav(document.getElementById('url').value)">Go</button>
  <span class="st"><span class="dot" id="dot"></span><span id="fps">-</span> fps</span>
</div>
<div class="wrap">
  <canvas id="c" tabindex="1"></canvas>
  <div class="info" id="info"></div>
</div>
<script>
const C=document.getElementById('c'),X=C.getContext('2d');
const dot=document.getElementById('dot'),fpsEl=document.getElementById('fps'),infoEl=document.getElementById('info');
let ws,natW=1280,natH=720,fc=0,lt=Date.now();

function conn(){
  ws=new WebSocket('ws://localhost:__PORT__/ws');
  ws.binaryType='arraybuffer';
  ws.onopen=()=>{dot.className='dot on';C.focus()};
  ws.onclose=()=>{dot.className='dot off';setTimeout(conn,1000)};
  ws.onmessage=e=>{
    if(e.data instanceof ArrayBuffer){
      const b=new Blob([e.data],{type:'image/jpeg'}),u=URL.createObjectURL(b),img=new Image;
      img.onload=()=>{
        if(C.width!==img.width||C.height!==img.height){
          C.width=img.width;C.height=img.height;
          natW=img.width;natH=img.height;
        }
        X.drawImage(img,0,0);URL.revokeObjectURL(u);fc++;
      };img.src=u;
    }
  };
}

setInterval(()=>{const n=Date.now(),s=(n-lt)/1000;fpsEl.textContent=Math.round(fc/s);fc=0;lt=n},2000);

// Map display pixel → screencast image pixel (what server expects)
function xy(e){
  const r=C.getBoundingClientRect();
  const x=(e.clientX-r.left)*(natW/r.width);
  const y=(e.clientY-r.top)*(natH/r.height);
  infoEl.textContent=Math.round(x)+','+Math.round(y);
  return{x,y};
}

function send(o){if(ws&&ws.readyState===1)ws.send(JSON.stringify(o))}

// Mouse — send mousedown/mouseup separately (not click — server handles it)
C.addEventListener('mousedown',e=>{
  e.preventDefault();C.focus();
  const p=xy(e);
  send({type:'mousedown',x:p.x,y:p.y,button:e.button});
});
C.addEventListener('mouseup',e=>{
  e.preventDefault();
  const p=xy(e);
  send({type:'mouseup',x:p.x,y:p.y,button:e.button});
});

// Also send click for simple cases (server does pressed+released)
C.addEventListener('click',e=>{
  e.preventDefault();
  const p=xy(e);
  send({type:'click',x:p.x,y:p.y,button:e.button});
});

C.addEventListener('dblclick',e=>{
  e.preventDefault();
  const p=xy(e);
  send({type:'dblclick',x:p.x,y:p.y,button:e.button});
});

let lm=0;
C.addEventListener('mousemove',e=>{
  const n=Date.now();if(n-lm<50)return;lm=n;
  const p=xy(e);
  send({type:'mousemove',x:p.x,y:p.y});
});

C.addEventListener('wheel',e=>{
  e.preventDefault();
  const p=xy(e);
  send({type:'scroll',x:p.x,y:p.y,deltaX:e.deltaX,deltaY:e.deltaY});
},{passive:false});

// Keyboard
C.addEventListener('keydown',e=>{
  if(document.activeElement===document.getElementById('url'))return;
  e.preventDefault();
  send({type:'keydown',key:e.key,code:e.code,shift:e.shiftKey,ctrl:e.ctrlKey,alt:e.altKey,meta:e.metaKey});
});
C.addEventListener('keyup',e=>{
  if(document.activeElement===document.getElementById('url'))return;
  e.preventDefault();
  send({type:'keyup',key:e.key,code:e.code,shift:e.shiftKey,ctrl:e.ctrlKey,alt:e.altKey,meta:e.metaKey});
});
C.addEventListener('contextmenu',e=>e.preventDefault());

function nav(u){
  if(!u.startsWith('http'))u='https://'+u;
  document.getElementById('url').value=u;
  fetch('/navigate?url='+encodeURIComponent(u));
}

conn();C.focus();
</script>
</body>
</html>`
