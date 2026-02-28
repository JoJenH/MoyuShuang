package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/nsf/termbox-go"
)

const (
	cacheFile = ".moyu_progress.json"
)

type Progress struct {
	LastLine   int `json:"last_line"`
	ViewHeight int `json:"view_height"`
}

var (
	// 内置硬核伪装日志
	dynamicLogs = []string{
		"[INFO] 初始化：魔芋爽全自动工业产线 (Build: 0.9.5-SPICY)",
		"[DEBUG] 压力检测：螺旋挤压机压力处于 150Mpa，符合《爽学》标准",
		"[INFO] 正在调配：卫龙秘制香辣红油 (配方号: RED_HOT_09)",
		"[WARN] 传感器报警：切片机 03 号刀片磨损度 85%，建议停机更换",
		"[INFO] 实时监测：魔芋精粉纯度 99.8%，Q弹系数检测中...",
		"[DEBUG] 注入二氧化碳... 模拟毛肚缝隙口感填充 (Sequence: BELLY_SIM)",
		"[ERROR] 故障：3 号搅拌池检测到非计划辣条侵入，正在自动拦截",
		"[INFO] 包装工序：正在抽真空，当前残氧量 < 0.01%",
		"[DEBUG] 生产效率：5000 包/小时，当前 MoyuShuang 贡献率：99%",
		"[INFO] 同步：正在将“爽度”数据上传至云端大数据分析中心",
		"[DEBUG] 检测到周边存在“老板”异常干扰信号，自动切换至静默运行模式...",
	}

	// 精确的手册说明
	helpDocs = []string{
		"SYSDIAG(8)                System Diagnostics Manual               SYSDIAG(8)",
		"NAME: MoyuShuang - Wojiuwen Ni Mode Shuang Bu'shuang",
		"",
		"CONTROLS:",
		"  j, MouseLeft, MouseDown   : Step forward in data stream (Next line)",
		"  k, MouseUp     : Step backward in data stream (Prev line)",
		"  Space     : Toggle BOSS_MODE (Immediate UI suspension)",
		"  /, G           : Search metadata / Jump to line offset",
		"  n, N           : Navigate through search matches",
		"  +, -           : Resize dynamic kernel buffer height",
		"  h, ?           : Show/Hide this diagnostic manual",
		"  Q              : Terminate daemon and sync state to cache",
		"",
		"NOTES: \"Moyushuang\" is a unit for measuring the degree of \"Shuang\".",
	}

	isSearching, isJumping, isHelpMode, searchActive, isBossMode = false, false, false, false, false
	searchQuery, jumpQuery, lastSearchQuery                      = "", "", ""
	searchResults                                                = []int{}
	matchIndex, viewHeight                                       = 0, 3
	progressStore                                                = make(map[string]Progress)
	bookPath                                                     string
	logOffset                                                    int
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <path_to_data> [optional_log_source]\n", os.Args[0])
		return
	}

	// 路径标准化：防止同文件多路径导致的进度冲突
	rawPath, _ := filepath.Abs(os.Args[1])
	if realPath, err := filepath.EvalSymlinks(rawPath); err == nil {
		bookPath = realPath
	} else {
		bookPath = rawPath
	}

	if len(os.Args) >= 3 {
		loadFakeLogs(os.Args[2])
	}

	if err := termbox.Init(); err != nil {
		panic(err)
	}
	termbox.SetInputMode(termbox.InputEsc | termbox.InputMouse)
	defer termbox.Close()

	loadProgress()
	if p, ok := progressStore[bookPath]; ok && p.ViewHeight > 0 {
		viewHeight = p.ViewHeight
	}

	lines, _ := loadAndWrapBook(bookPath)
	currentLine := progressStore[bookPath].LastLine
	if currentLine >= len(lines) {
		currentLine = 0
	}

	for {
		drawUI(currentLine, lines)

		ev := termbox.PollEvent()
		oldLine := currentLine

		switch ev.Type {
		case termbox.EventResize:
			lines, _ = loadAndWrapBook(bookPath)
		case termbox.EventMouse:
			if !isBossMode && !isSearching && !isJumping && !isHelpMode {
				switch ev.Key {
				case termbox.MouseWheelDown, termbox.MouseLeft:
					if currentLine < len(lines)-1 {
						currentLine++
					}
				case termbox.MouseWheelUp:
					if currentLine > 0 {
						currentLine--
					}
				}
			}
		case termbox.EventKey:
			// Boss 键拥有最高优先级
			if ev.Key == termbox.KeySpace {
				isBossMode = !isBossMode
				isHelpMode, isSearching, isJumping = false, false, false
				continue
			}
			if isBossMode {
				continue
			}

			if isSearching {
				handleSearchInput(ev, &currentLine, lines)
				continue
			}
			if isJumping {
				handleJumpInput(ev, &currentLine, len(lines))
				continue
			}

			switch ev.Ch {
			case 'Q':
				saveProgress(currentLine)
				return
			case 'h', '?':
				isHelpMode = !isHelpMode
			case 'j':
				if currentLine < len(lines)-1 {
					currentLine++
				}
			case 'k':
				if currentLine > 0 {
					currentLine--
				}
			case '/':
				isSearching, searchQuery, isHelpMode = true, "", false
			case 'G':
				isJumping, jumpQuery, isHelpMode = true, "", false
			case 'n':
				if searchActive && len(searchResults) > 0 {
					matchIndex = (matchIndex + 1) % len(searchResults)
					currentLine = searchResults[matchIndex]
				}
			case 'N':
				if searchActive && len(searchResults) > 0 {
					matchIndex = (matchIndex - 1 + len(searchResults)) % len(searchResults)
					currentLine = searchResults[matchIndex]
				}
			case '+', '=':
				if viewHeight < 12 {
					viewHeight++
				}
			case '-', '_':
				if viewHeight > 1 {
					viewHeight--
				}
			}

			if ev.Key == termbox.KeyEsc {
				isHelpMode, isSearching, isJumping, searchActive = false, false, false, false
			}
		}

		// 翻页带动日志演进
		if currentLine != oldLine {
			if rand.Intn(10) < 4 {
				logOffset++
			}
		}
	}
}

func drawUI(currentLine int, lines []string) {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	w, h := termbox.Size()

	// 1. 日志区渲染
	logAreaH := h - viewHeight - 3
	if isHelpMode {
		logAreaH = h - len(helpDocs) - 3
	}

	for i := 0; i < logAreaH; i++ {
		lIdx := (i + logOffset) % len(dynamicLogs)
		content := dynamicLogs[lIdx]

		fg := termbox.ColorCyan
		if strings.Contains(content, "[WARN]") {
			fg = termbox.ColorYellow
		} else if strings.Contains(content, "[ERROR]") {
			fg = termbox.ColorRed
		} else if strings.Contains(content, "[DEBUG]") {
			fg = termbox.ColorDarkGray
		}

		drawText(0, i, content, fg, termbox.ColorDefault)
	}

	// 2. 状态分隔行
	statusY := h - viewHeight - 2
	if isHelpMode {
		statusY = h - len(helpDocs) - 2
	}

	statusLine := fmt.Sprintf("--- STATE: RUNNING | Mo-Shuang Scale: %d%% | ID: %d/%d ", (currentLine+1)*100/len(lines), currentLine+1, len(lines))
	if isBossMode {
		statusLine = "--- STATE: SUSPENDED (KERNEL_WAIT) ---"
	}
	drawText(0, statusY, statusLine, termbox.ColorBlue, termbox.ColorDefault)

	// 3. 底部指令/阅读区
	if isBossMode {
		drawText(0, h-1, ">> [IDLE] Awaiting SIGCONT...", termbox.ColorDarkGray, termbox.ColorDefault)
	} else if isHelpMode {
		for i, doc := range helpDocs {
			drawText(0, h-len(helpDocs)+i, doc, termbox.ColorGreen, termbox.ColorDefault)
		}
	} else if isSearching {
		drawText(0, h-1, "GREP_SCAN: /"+searchQuery, termbox.ColorYellow|termbox.AttrBold, termbox.ColorDefault)
	} else if isJumping {
		drawText(0, h-1, "ADDR_JUMP: "+jumpQuery, termbox.ColorMagenta|termbox.AttrBold, termbox.ColorDefault)
	} else {
		for i := 0; i < viewHeight; i++ {
			idx := currentLine + i
			if idx < len(lines) {
				drawText(0, h-viewHeight+i, ">> "+lines[idx], termbox.ColorBlack|termbox.AttrBold, termbox.ColorDefault)
			}
		}
	}

	termbox.SetCursor(w-1, h-1)
	termbox.Flush()
}

// --- 核心工具函数 ---

func drawText(x, y int, str string, fg, bg termbox.Attribute) {
	w, _ := termbox.Size()
	currX := x
	for _, r := range str {
		if currX >= w-1 {
			break
		}
		termbox.SetCell(currX, y, r, fg, bg)
		currX += runewidth.RuneWidth(r)
	}
}

func loadAndWrapBook(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return []string{"ERR: STREAM_NOT_FOUND"}, err
	}
	defer file.Close()
	w, _ := termbox.Size()
	maxW := w - 8
	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			allLines = append(allLines, "")
			continue
		}
		runes := []rune(text)
		var b strings.Builder
		currW := 0
		for _, r := range runes {
			rw := runewidth.RuneWidth(r)
			if currW+rw > maxW {
				allLines = append(allLines, b.String())
				b.Reset()
				currW = 0
			}
			b.WriteRune(r)
			currW += rw
		}
		allLines = append(allLines, b.String())
	}
	return allLines, nil
}

func handleSearchInput(ev termbox.Event, currentLine *int, lines []string) {
	if ev.Key == termbox.KeyEnter {
		isSearching = false
		if searchQuery != "" {
			lastSearchQuery, searchActive = searchQuery, true
			updateSearchResults(lines)
			if len(searchResults) > 0 {
				matchIndex = 0
				for i, lineIdx := range searchResults {
					if lineIdx >= *currentLine {
						matchIndex = i
						*currentLine = lineIdx
						break
					}
				}
			}
		}
	} else if ev.Key == termbox.KeyEsc {
		isSearching = false
	} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
		if len(searchQuery) > 0 {
			searchQuery = searchQuery[:len(searchQuery)-1]
		}
	} else if ev.Ch != 0 {
		searchQuery += string(ev.Ch)
	}
}

func handleJumpInput(ev termbox.Event, currentLine *int, totalLines int) {
	if ev.Key == termbox.KeyEnter {
		isJumping = false
		var target int
		fmt.Sscanf(jumpQuery, "%d", &target)
		if target > 0 && target <= totalLines {
			*currentLine = target - 1
		}
	} else if ev.Key == termbox.KeyEsc {
		isJumping = false
	} else if ev.Key == termbox.KeyBackspace || ev.Key == termbox.KeyBackspace2 {
		if len(jumpQuery) > 0 {
			jumpQuery = jumpQuery[:len(jumpQuery)-1]
		}
	} else if ev.Ch >= '0' && ev.Ch <= '9' {
		jumpQuery += string(ev.Ch)
	}
}

func updateSearchResults(lines []string) {
	searchResults = []int{}
	query := strings.ToLower(lastSearchQuery)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			searchResults = append(searchResults, i)
		}
	}
}

func saveProgress(line int) {
	p := Progress{LastLine: line, ViewHeight: viewHeight}
	progressStore[bookPath] = p
	data, _ := json.Marshal(progressStore)
	home, _ := os.UserHomeDir()
	_ = os.WriteFile(filepath.Join(home, cacheFile), data, 0644)
}

func loadProgress() {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, cacheFile))
	if err == nil {
		json.Unmarshal(data, &progressStore)
	}
}

func loadFakeLogs(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	var newLogs []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			newLogs = append(newLogs, line)
		}
	}
	if len(newLogs) > 0 {
		dynamicLogs = newLogs
	}
}
