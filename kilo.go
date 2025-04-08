package main

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

var (
	ErrExitTerminal = errors.New("exit terminal")
)

const (
	// SPECIAL CHARACTERS
	BACKSPACE = 127
	ENTER     = 13
	ExitCode  = 17
	Ctrl_L    = 12
	Ctrl_H    = 8
	Ctrl_F    = 6
	Esc       = 27
	Ctrl_S    = 19
	SpaceBar  = 32

	// constants
	KILO_VERSION    = "0.0.1"
	KILO_TAB_STOP   = 8
	KILO_QUIT_TIMES = 3

	// EDITOR KEYS
	ARROW_UP = iota + 1_114_112
	ARROW_DOWN
	ARROW_LEFT
	ARROW_RIGHT
	PAGE_UP
	PAGE_DOWN
	HOME_KEY
	END_KEY
	DEL_KEY

	// raw mode options
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA

	// syntax highlighting colors
	HL_NORMAL uint8 = 0
	HL_NUMBER uint8 = 1
	HL_MATCH  uint8 = 2

	// ANSI Color Codes
	ColorRed   = 31
	ColorBlack = 30
	ColorWhite = 37
	ColorBlue  = 34

	HL_HIGHLIGHT_NUMBERS  = 1 << 0
	HL_HIGHLIGHT_STRINGS  = 1 << 1
	HL_HIGHLIGHT_COMMENTS = 1 << 2
)

var (
	lastMatch       = -1
	direction       = 1
	savedHLLine     = 0
	savedHL         = []uint8{}
	quitkeyPresses  = KILO_QUIT_TIMES
	C_HL_extension  = []string{".c", ".h", ".cpp"}
	Go_HL_extension = []string{".go"}

	// HLDB stands for “highlight database”
	HL_DB = []editorSyntax{
		{
			fileType:  "c",
			fileMatch: C_HL_extension,
			flags:     HL_HIGHLIGHT_NUMBERS,
		},
		{
			fileType:  "go",
			fileMatch: Go_HL_extension,
			flags:     HL_HIGHLIGHT_NUMBERS,
		},
	}
)

type eRow struct {
	// Size of the original line/row in characters
	// Set when row is created in editorAppendRow
	size int

	// Original content of the line/row
	// Set when row is created in editorAppendRow
	chars string

	// Rendered content of the line (with tabs expanded)
	// Set in editorUpdateRow when tabs are processed
	render string

	// Size of the rendered content
	// Set in editorUpdateRow after render string is created
	rsize int

	// syntax highlighting format
	hl []uint8 // we only need 0 to 255
}

type EditorConfig struct {
	// Original terminal settings to restore on exit
	// Set once during initEditor
	origTermios *State

	// Terminal window size (rows and columns)
	// Set during initEditor, used for display boundaries
	winSize *unix.Winsize

	// Current cursor position - horizontal (column)
	// - Incremented/decremented in editorMoveCursor:
	// - Decrements when moving left
	// - Increments when moving right
	// - Resets to 0 on END_KEY
	// - Sets to window width-1 on HOME_KEY
	cursorX int

	// Current cursor position - vertical (row)
	// Incremented/decremented in editorMoveCursor:
	// - Decrements on ARROW_UP
	// - Increments on ARROW_DOWN
	// - Changes on PAGE_UP/PAGE_DOWN
	cursorY int

	// Total number of rows in the file
	// Incremented in editorAppendRow when adding new rows
	numRows int

	// Array of rows containing file content
	// Appended to in editorAppendRow when reading file
	rows []eRow

	// Row offset for vertical scrolling
	// Updated in editorScroll based on cursor position
	// Changes when cursor moves outside visible area
	rowOff int

	// Column offset for horizontal scrolling
	// Updated in editorScroll based on rowX position
	// Changes when cursor moves outside visible area
	colOff int

	// Current render X position (accounts for tabs)
	// Updated in editorScroll
	// Calculated from cursorX considering tab expansion
	rowX int

	// Current file being edited or viewed
	fileName string

	// This is for displaying messages to the user, and prompting
	// the user for input when doing a search, for example
	statusMsg string

	// Timestamp for statusMsg, so that we can erase it a few seconds
	// after it’s been displayed.
	statusMsgTime time.Time

	// Tells us if the file has been modified since it was opened or saved
	dirty bool
	
	syntax *editorSyntax
}

type state struct {
	termios unix.Termios
}

type State struct {
	state
}

type editorSyntax struct {
	// The filetype field is the name of the filetype that will be
	// displayed to the user in the status bar
	fileType string

	// filematch is an array of strings, where each string contains a pattern
	// to match a filename against. If the filename matches, then the file will
	// be recognized as having that filetype.
	fileMatch []string

	// Finally, flags is a bit field that will contain flags for whether
	// to highlight numbers and whether to highlight strings for that filetype
	flags int
}

type callback func(query string, lastKeyPressed int)

func main() {

	var fileName string
	flag.StringVar(&fileName, "filename", "", "enter file to edit")
	flag.Parse()

	fd := int(os.Stdin.Fd())

	oldState, err := enableRawMode(fd)
	if err != nil {
		log.Fatal(err)
	}
	defer restore(fd, oldState)

	config, err := initEditor(fd, oldState)
	if err != nil {
		die(err)
		return
	}

	if fileName != "" {
		err = editorOpen(config, fileName)
		if err != nil {
			die(err)
			return
		}
	}

	editorSetStatusMessage(config, "HELP: Ctrl-S = save | Ctrl-Q = quit | Ctrl-F = find")

	for {
		editorRefreshScreen(config)
		err = editorProcessKeyPress(config)
		if errors.Is(err, ErrExitTerminal) {
			break
		}

		if err != nil {
			die(err)
			break
		}
	}
}

// *** Editor Operations

func editorInsertRow(config *EditorConfig, line string, at int) {

	if at < 0 || at > len(config.rows) {
		return
	}

	row := eRow{
		size:  len(line),
		chars: line,
	}

	editorUpdateRow(&row)

	config.numRows++

	if at == len(config.rows) {
		config.rows = append(config.rows, row)
		return
	}

	config.rows = slices.Insert(config.rows, at, row)
}

func editorUpdateRow(row *eRow) {
	var b strings.Builder

	tabs := 0

	for _, r := range row.chars {
		if r == '\t' {
			tabs++
		}
	}

	// add 8 characters per tab
	idx := 0
	for _, r := range row.chars {
		if r == '\t' {
			b.WriteString(" ")
			idx++
			for idx%KILO_TAB_STOP != 0 {
				b.WriteString(" ")
				idx++
			}
		} else {
			b.WriteRune(r)
		}
	}

	row.render = b.String()
	row.rsize = len(row.render)
	row.hl = make([]uint8, row.rsize)
	editorUpdateSyntax(row)
}

func editorRowInsertChar(row *eRow, at, key int) {
	if at < 0 || at > row.size {
		at = row.size
	}

	row.chars = row.chars[:at] + fmt.Sprintf("%c", rune(key)) + row.chars[at:]
	row.size = len(row.chars)
	editorUpdateRow(row)
}

func editorRowDelChar(row *eRow, at int) {
	if at < 0 || at >= row.size {
		return
	}

	row.chars = row.chars[:at] + row.chars[at+1:row.size]
	row.size = len(row.chars)
	editorUpdateRow(row)
}

func editorDelChar(cfg *EditorConfig) {
	if cfg.cursorY == cfg.numRows {
		return
	}

	if cfg.cursorX == 0 && cfg.cursorY == 0 {
		return
	}

	cfg.dirty = true

	currentRow := &cfg.rows[cfg.cursorY]
	if cfg.cursorX > 0 {
		editorRowDelChar(currentRow, cfg.cursorX-1)
		cfg.cursorX--
		return
	}

	prevRow := &cfg.rows[cfg.cursorY-1]
	cfg.cursorX = prevRow.size
	editorRowAppendString(cfg, prevRow, currentRow.chars)
	editorDelRow(cfg, cfg.cursorY)
	cfg.cursorY--
}

func editorDelRow(cfg *EditorConfig, at int) {
	if at < 0 || at > cfg.numRows {
		return
	}

	cfg.rows = append(cfg.rows[:at], cfg.rows[at+1:len(cfg.rows)]...)
	cfg.numRows--
}

func editorRowAppendString(cfg *EditorConfig, row *eRow, text string) {
	row.chars = row.chars + text
	row.size = len(row.chars)
	editorUpdateRow(row)
	cfg.dirty = true
}

func editorInsertChar(cfg *EditorConfig, key int) {
	if cfg.cursorY == cfg.numRows {
		if cfg.cursorY == 0 {
			editorInsertRow(cfg, "", 0)
		} else {
			editorInsertRow(cfg, "", cfg.cursorY-1)
		}
	}
	editorRowInsertChar(&cfg.rows[cfg.cursorY], cfg.cursorX, key)
	cfg.cursorX++
	cfg.dirty = true
}

func editorInsertNewLine(cfg *EditorConfig) {
	if cfg.cursorX == 0 {
		editorInsertRow(cfg, "", cfg.cursorY-1)
		cfg.cursorY++
		return
	}

	row := &cfg.rows[cfg.cursorY]
	editorInsertRow(cfg, row.chars[cfg.cursorX:len(row.chars)], cfg.cursorY+1)

	row.chars = row.chars[:cfg.cursorX]
	row.size = len(row.chars)
	editorUpdateRow(row)
	cfg.cursorX = 0
	cfg.cursorY++
}

//*** drawing editor functions

func editorDrawStatusBar(cfg *EditorConfig, buf *bytes.Buffer) {
	// To make the status bar stand out, we’re going to display it with
	// inverted colors: black text on a white background. The escape sequence
	// <esc>[7m switches to inverted colors, and <esc>[m switches back to
	// normal formatting
	buf.WriteString("\x1b[7m")
	status := fmt.Sprintf("%.20s - %d lines", cmp.Or(cfg.fileName, "[No Name]"), cfg.numRows)
	if cfg.dirty {
		status = fmt.Sprintf("%s %s", status, "(modified)")
	}
	buf.WriteString(status)
	rStatus := fmt.Sprintf("%d/%d", cfg.cursorY+1, cfg.numRows)
	length := len(status)

	if length > int(cfg.winSize.Col) {
		length = int(cfg.winSize.Col)
	}

	for length < int(cfg.winSize.Col) {
		if int(cfg.winSize.Col)-len(rStatus) == length {
			buf.WriteString(rStatus)
			break
		}
		buf.WriteString(" ")
		length++
	}

	buf.WriteString("\x1b[m")
	buf.Write([]byte("\r\n"))
}

func editorDrawMessageBar(cfg *EditorConfig, buf *bytes.Buffer) {
	buf.WriteString("\x1b[K")
	msgLen := len(cfg.statusMsg)
	if msgLen > int(cfg.winSize.Col) {
		msgLen = int(cfg.winSize.Col)
	}

	if msgLen > 0 && time.Now().Sub(cfg.statusMsgTime) < 5*time.Second {
		buf.WriteString(cfg.statusMsg)
	}
}

func editorDrawRows(cfg *EditorConfig, buf *bytes.Buffer) {
	var y uint16
	for y = 0; y < cfg.winSize.Row; y++ {
		fileRow := cfg.rowOff + int(y)
		if fileRow >= cfg.numRows {
			if y == cfg.winSize.Row/3 && cfg.numRows == 0 {
				message := fmt.Sprintf("Kilo editor -- version %s", KILO_VERSION)
				end := uint16(len(message))

				// try not to go past the screen
				if end > cfg.winSize.Col {
					end = cfg.winSize.Col
				}

				// center the welcome text
				padding := (cfg.winSize.Col - end) / 2
				if padding > 0 {
					buf.Write([]byte("~"))
					padding--
				}

				for padding > 0 {
					buf.Write([]byte(" "))
					padding--
				}

				buf.Write([]byte(message[:end]))
			} else {
				buf.Write([]byte("~"))
			}
		} else {
			row := cfg.rows[fileRow]
			length := row.rsize - cfg.colOff
			if length < 0 {
				length = 0
			}
			// we do not want to write past the screen
			if length > int(cfg.winSize.Col) {
				length = int(cfg.winSize.Col)
			}

			hl := row.hl
			currentColor := -1

			for i, r := range row.render[cfg.colOff : cfg.colOff+length] {
				if hl[i] == HL_NORMAL {
					if currentColor != -1 {
						buf.WriteString("\x1b[39m")
						currentColor = -1
					}
					buf.WriteRune(r)
				} else {
					color := editorSyntaxToColor(hl[i])
					if currentColor != int(color) {
						buf.WriteString(fmt.Sprintf("\x1b[%dm", color))
						currentColor = int(color)
					}
					buf.WriteRune(r)
				}
			}
			buf.WriteString("\x1b[39m")

			// if length > 0 {
			// 	buf.WriteString(row.render[cfg.colOff : cfg.colOff+length])
			// }
		}
		buf.Write([]byte("\x1b[K"))
		buf.Write([]byte("\r\n"))
	}
}

// *** Editor manage cursor position
func editorCursorXToRowX(row eRow, cursorX int) int {
	rx := 0

	for j := 0; j < cursorX; j++ {
		if row.chars[j] == '\t' {
			rx += (KILO_TAB_STOP - 1) - (rx % KILO_TAB_STOP)
		}
		rx++
	}

	return rx
}

func editorRowXToCursorX(row eRow, rx int) int {
	cur_rx := 0

	var cx int

	for cx = 0; cx < row.size; cx++ {
		if row.chars[cx] == '\t' {
			cur_rx += (KILO_TAB_STOP - 1) - (cur_rx % KILO_TAB_STOP)
		}
		cur_rx++

		if cur_rx > rx {
			return cx
		}
	}

	return cx
}

func editorScroll(cfg *EditorConfig) {
	cfg.rowX = 0

	if cfg.cursorY < cfg.numRows {
		cfg.rowX = editorCursorXToRowX(cfg.rows[cfg.cursorY], cfg.cursorX)
	}

	if cfg.cursorY < cfg.rowOff {
		cfg.rowOff = cfg.cursorY
	}

	if cfg.cursorY >= cfg.rowOff+int(cfg.winSize.Row) {
		cfg.rowOff = cfg.cursorY - int(cfg.winSize.Row) + 1
	}

	if cfg.rowX < cfg.colOff {
		cfg.colOff = cfg.rowX
	}

	if cfg.rowX >= cfg.colOff+int(cfg.winSize.Col) {
		cfg.colOff = cfg.rowX - int(cfg.winSize.Col) + 1
	}
}

func editorRefreshScreen(cfg *EditorConfig) {
	editorScroll(cfg)

	var buf bytes.Buffer

	// hide cursor
	buf.Write([]byte("\x1b[?25l"))
	buf.Write([]byte("\x1b[H"))

	editorDrawRows(cfg, &buf)
	editorDrawStatusBar(cfg, &buf)
	editorDrawMessageBar(cfg, &buf)

	// move cursor
	buf.Write([]byte(fmt.Sprintf("\x1b[%d;%dH", (cfg.cursorY-cfg.rowOff)+1, (cfg.rowX-cfg.colOff)+1)))

	// show cursor
	buf.Write([]byte("\x1b[?25h"))
	os.Stdout.Write(buf.Bytes())
}

// *** process key presses
func editorProcessKeyPress(cfg *EditorConfig) error {
	key, err := editorReadKey()
	if err != nil {
		return fmt.Errorf("processing key press: %w", err)
	}

	switch key {
	case ExitCode:
		if cfg.dirty && quitkeyPresses > 0 {
			editorSetStatusMessage(cfg, `WARNING!!! File has unsaved changes. Press Ctrl-Q %d more times to quit.`, quitkeyPresses)
			quitkeyPresses--
			return nil
		}
		return ErrExitTerminal
	case ARROW_UP, ARROW_DOWN, ARROW_RIGHT, ARROW_LEFT:
		editorMoveCursor(key, cfg)
	case PAGE_DOWN, PAGE_UP:
		{
			if key == PAGE_UP {
				cfg.cursorY = cfg.rowOff
			} else if key == PAGE_DOWN {
				cfg.cursorY = cfg.rowOff + int(cfg.winSize.Row) - 1
			}

			if cfg.cursorY >= cfg.numRows {
				cfg.cursorY = cfg.numRows
			}
			i := cfg.winSize.Row
			for i != 0 {
				if key == PAGE_UP {
					editorMoveCursor(ARROW_UP, cfg)
				} else {
					editorMoveCursor(ARROW_DOWN, cfg)
				}
				i--
			}
		}
	case HOME_KEY:
		if cfg.cursorY < cfg.numRows {
			cfg.cursorX = cfg.rows[cfg.cursorY].size
		}
	case END_KEY:
		cfg.cursorX = 0
	case BACKSPACE, Ctrl_H:
		editorDelChar(cfg)
	case ENTER:
		editorInsertNewLine(cfg)
	case Ctrl_L, Esc:
		break
	case Ctrl_S:
		editorSave(cfg)
	case Ctrl_F:
		editorSearch(cfg)
	default:
		editorInsertChar(cfg, key)
	}

	quitkeyPresses = KILO_QUIT_TIMES
	return nil
}

func editorMoveCursor(key int, cfg *EditorConfig) {
	var row eRow
	if cfg.cursorY < cfg.numRows {
		row = cfg.rows[cfg.cursorY]
	}

	switch key {
	case ARROW_UP:
		if cfg.cursorY > 0 {
			cfg.cursorY--
		}
	case ARROW_DOWN:
		if cfg.cursorY < cfg.numRows {
			cfg.cursorY++
		}

	case ARROW_LEFT:
		if cfg.cursorX > 0 {
			cfg.cursorX--
		} else if cfg.cursorY > 0 {
			cfg.cursorY--
			cfg.cursorX = cfg.rows[cfg.cursorY].size
		}

	case ARROW_RIGHT:
		if cfg.cursorX < row.size {
			cfg.cursorX++
		} else if row.size == cfg.cursorX && cfg.cursorY != cfg.numRows {
			cfg.cursorY++
			cfg.cursorX = 0
		}
	}

	// row could have changed here, arrow left and arrow right
	// could have altered the row
	if cfg.cursorY < cfg.numRows {
		row = cfg.rows[cfg.cursorY]
	}

	if cfg.cursorX > row.size {
		cfg.cursorX = row.size
	}
}

func editorReadKey() (int, error) {
	reader := bufio.NewReader(os.Stdin)
	r, _, err := reader.ReadRune()
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading key: %w", err)
	}

	escapeKeys := []rune{1, 4, 5, 6, 7, 8}

	if r == 27 {
		// read next two runes to complete escape sequence
		r, _, _ = reader.ReadRune() // r is likely [
		if r == '[' || r == 'O' {   // an escape sequence, continue
			r, _, _ = reader.ReadRune()         // sequence
			if slices.Contains(escapeKeys, r) { // maybe a page up or down
				next, _, _ := reader.ReadRune() // read next one
				if next != '~' {                // not a page up or down
					r = next
				}
			}
		}
	}

	switch r {
	case 'A':
		return ARROW_UP, nil
	case 'B':
		return ARROW_DOWN, nil
	case 'C':
		return ARROW_RIGHT, nil
	case 'D':
		return ARROW_LEFT, nil
	case 53:
		return PAGE_UP, nil
	case 54:
		return PAGE_DOWN, nil
	case 70:
		return HOME_KEY, nil
	case 72:
		return END_KEY, nil
	case 517:
		return DEL_KEY, nil
	default:
		unicodeValue, _ := strconv.Atoi(fmt.Sprintf("%d", r))
		return unicodeValue, nil
	}
}

//*** Editor Setup

func initEditor(fd int, oldState *State) (*EditorConfig, error) {
	winSize, err := getWindowSize(fd)
	if err != nil {
		return nil, fmt.Errorf("getting window size: %w", err)
	}

	config := EditorConfig{
		origTermios: oldState,
		winSize:     winSize,
	}

	// We decrement config.winSize.Row so that editorDrawRows() doesn’t try to
	// draw a line of text at the bottom of the screen
	config.winSize.Row -= 2

	return &config, nil
}

func getCursorPosition() (rows, cols int) {
	buf := make([]byte, 1)
	// The n command (Device Status Report) can be used to query the
	// terminal for status information. We want to give it an argument
	// of 6 to ask for the cursor position.
	os.Stdout.Write([]byte("\x1b[6n"))
	var s bytes.Buffer
	var b byte

	// Then we can read the reply from the standard input.
	// expected structure: `\x1b[24;80R`
	for b != 'R' {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}

		b = buf[0]
		s.WriteByte(b)
	}

	// we want to skip the escape sequence byte
	response := string(s.Bytes()[1 : s.Len()-1])
	fmt.Sscanf(response, "[%d;%d", &rows, &cols)

	return rows, cols
}

func getWindowSize(fd int) (*unix.Winsize, error) {
	size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		// we are using the C [Cursor Forward] and B [Cursor Down]
		// commands
		os.Stdout.Write([]byte("\x1b[999C\x1b[999B"))
		rows, cols := getCursorPosition()
		size = &unix.Winsize{
			Row: uint16(rows),
			Col: uint16(cols),
		}
		return size, nil

	}
	return size, nil
}

func enableRawMode(fd int) (*State, error) {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}

	oldState := State{state{termios: *termios}}

	// This attempts to replicate the behaviour documented for cfmakeraw in
	// the termios(3) manpage.
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	termios.Oflag &^= unix.OPOST
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, termios); err != nil {
		return nil, err
	}

	return &oldState, nil
}

func restore(fd int, state *State) error {
	return unix.IoctlSetTermios(fd, ioctlWriteTermios, &state.termios)
}

func editorSetStatusMessage(cfg *EditorConfig, format string, args ...any) {
	cfg.statusMsg = fmt.Sprintf(format, args...)
	cfg.statusMsgTime = time.Now()
}

// *** Utils
func isControl(b byte) bool {
	return b >= 0 && (b < 32 || b == 127)
}

func die(err error) {
	var buf bytes.Buffer
	buf.Write([]byte("\x1b[2J"))
	buf.Write([]byte("\x1b[H"))
	os.Stdout.Write(buf.Bytes())
	log.Print(err)
}

func isSeparator(c int32) int32 {
	separators := ",.()+-/*=~%<>[];"
	if c == SpaceBar || strings.Contains(separators, fmt.Sprintf("%c", c)) {
		return c
	}
	return 0
}

// *** file i/o

func editorOpen(config *EditorConfig, fileName string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", fileName, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimRight(line, "\r")
		editorInsertRow(config, line, config.numRows)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	config.fileName = fileName

	return nil
}

func editorRowsToString(cfg *EditorConfig) string {
	var buf bytes.Buffer
	for _, row := range cfg.rows {
		buf.WriteString(row.chars)
		buf.WriteByte('\n')
	}

	return buf.String()
}

func editorSave(cfg *EditorConfig) {
	if cfg.fileName == "" {
		cfg.fileName = editorPrompt(cfg, "Save as")
		if cfg.fileName == "" {
			editorSetStatusMessage(cfg, "Save aborted")
			return
		}
	}

	contents := editorRowsToString(cfg)
	err := os.WriteFile(cfg.fileName, []byte(contents), 0644)

	if err != nil {
		editorSetStatusMessage(cfg, "Can't save! I/O error: %s", err.Error())
		return
	}

	editorSetStatusMessage(cfg, "%d bytes written to disk", len(contents))
	cfg.dirty = false
}

func editorFindCallback(cfg *EditorConfig, query string) {
	if len(savedHL) > 0 {
		cfg.rows[savedHLLine].hl = savedHL
		savedHL = []uint8{}
	}

	if query == "" {
		return
	}

	if lastMatch == -1 {
		direction = 1
	}

	current := lastMatch

	for range cfg.numRows {
		current += direction
		if current == -1 {
			current = cfg.numRows - 1
		} else if current == cfg.numRows {
			current = 0
		}

		row := &cfg.rows[current]
		if strings.Contains(row.render, query) {
			cfg.cursorY = current
			lastMatch = current
			savedHLLine = current
			savedHL = make([]uint8, len(row.hl))
			copy(savedHL, row.hl)

			index := strings.Index(row.render, query)
			for i := range query {
				row.hl[index+i] = HL_MATCH
			}
			cfg.cursorX = editorRowXToCursorX(*row, index+len(query)-1)
			cfg.rowOff = cfg.numRows

			break
		}
	}
}

func editorSearch(cfg *EditorConfig) {
	savedCursorX := cfg.cursorX
	savedCursorY := cfg.cursorY
	savedColOff := cfg.colOff
	savedRowOff := cfg.rowOff

	r := editorPrompt(cfg, "Search: %s (Use ESC/Arrows/Enter)", func(query string, key int) {
		if key == ARROW_RIGHT || key == ARROW_DOWN {
			direction = 1
		} else if key == ARROW_UP || key == ARROW_LEFT {
			direction = -1
		} else {
			direction = 1
			lastMatch = -1
		}
		editorFindCallback(cfg, query)
	})

	if r == "" {
		cfg.cursorX = savedCursorX
		cfg.cursorY = savedCursorY
		cfg.colOff = savedColOff
		cfg.rowOff = savedRowOff
		editorFindCallback(cfg, r)
	} else {
		lastMatch = -1
	}

}

func editorPrompt(cfg *EditorConfig, prompt string, cb ...callback) string {
	var buf strings.Builder

	var fn callback = nil

	if len(cb) > 0 {
		fn = cb[0]
	}

	for {
		editorSetStatusMessage(cfg, "%s: Press esc to exit: %s", prompt, buf.String())
		editorRefreshScreen(cfg)

		c, err := editorReadKey()
		if err != nil {
			continue
		}

		if c == ENTER {
			if buf.String() != "" {
				editorSetStatusMessage(cfg, "%s", "")
				return buf.String()
			}
		}

		if c == Esc {
			return ""
		}

		if c == BACKSPACE && buf.String() != "" {
			current := buf.String()
			buf.Reset()
			buf.WriteString(current[:len(current)-1])
			fn(buf.String(), c)
			continue
		}

		if isControl(byte(c)) {
			continue
		}

		buf.WriteRune(rune(c))

		fn(buf.String(), c)
	}
}

/** Syntax Highlighting */

func editorUpdateSyntax(row *eRow) {
	// we do not need to do memset because we created the Go slice with a length
	// which will initialise all values to the zero value

	prevSep := int32(1)
	for i, r := range row.render {
		prevHL := HL_NORMAL
		if i > 0 {
			prevHL = row.hl[i-1]
		}

		if unicode.IsDigit(r) && (prevSep != 0 || prevHL == HL_NUMBER) || r == '.' && prevHL == HL_NUMBER {
			row.hl[i] = HL_NUMBER
			prevSep = 0
			continue
		}

		prevSep = isSeparator(r)
	}
}

func editorSyntaxToColor(hl uint8) uint8 {
	switch hl {
	case HL_NUMBER:
		return ColorRed
	case HL_MATCH:
		return ColorBlue
	default:
		return ColorWhite
	}
}
