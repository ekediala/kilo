# Kilo Text Editor (Go Version)

This project is a simple terminal-based text editor written in Go. It is heavily inspired by and follows the logic presented in the classic [Kilo editor](https://viewsourcecode.org/snaptoken/kilo/) by Salvatore Sanfilippo (antirez), originally written in C. This version adapts the concepts to Go, utilizing the `golang.org/x/sys/unix` package for low-level terminal interactions on Unix-like systems.

## Features

*   **Terminal Raw Mode:** Manipulates terminal settings for direct key input processing.
*   **Basic Text Editing:** Insert characters, delete characters (Backspace), insert new lines (Enter).
*   **File I/O:** Open existing files, save changes (`Ctrl-S`), create new files.
*   **Cursor Movement:** Navigate using Arrow Keys, Page Up/Down, Home/End.
*   **Scrolling:** Handles vertical and horizontal scrolling when content exceeds window size.
*   **Status Bar:** Displays filename, line count, current position, and dirty status (unsaved changes).
*   **Message Bar:** Shows informational messages (e.g., save confirmations, help, prompts).
*   **Incremental Search:** Find text within the file using `Ctrl-F`. Navigate matches with Arrow Keys during search.
*   **Basic Syntax Highlighting:**
    *   Highlights numbers.
    *   Highlights search matches temporarily.
    *   Rudimentary filetype detection for `.c`, `.h`, `.cpp`, `.go` (currently just enables number highlighting).
*   **Quit Confirmation:** Warns if attempting to quit (`Ctrl-Q`) with unsaved changes.
*   **Tab Expansion:** Renders tab characters as a configurable number of spaces (default: 8).
*   **Clean Exit:** Restores original terminal settings on exit.

## Requirements

*   Go version 1.23 or later (as specified in `go.mod`).
*   A Unix-like operating system (Linux, macOS, BSD) - due to the use of `golang.org/x/sys/unix` for terminal control.

## Installation

**Using `go install`:**

```bash
go install github.com/ekediala/kilo@latest
```

Ensure your Go bin directory (`$GOPATH/bin` or `$HOME/go/bin`) is in your system's `PATH`.

**Building from Source:**

```bash
git clone https://github.com/ekediala/kilo.git
cd kilo
go build -o kilo .
# Now you can run ./kilo
```

## Usage

**Start the editor:**

```bash
# Start with an empty buffer
./kilo

# Open an existing file or create a new one
./kilo <filename>
```

**Example:**

```bash
./kilo main.go
```

## Key Bindings

*   `Ctrl-Q`: Quit the editor. If the file has unsaved changes, you'll be prompted to press `Ctrl-Q` multiple times to confirm.
*   `Ctrl-S`: Save the current file. If the file is new, you'll be prompted for a filename.
*   `Ctrl-F`: Enter search mode.
    *   Type your search query.
    *   Use `Enter` to confirm the search and stay at the current match.
    *   Use `Esc` to cancel the search and return to the original position.
    *   Use `Arrow Up/Left` to find the previous match.
    *   Use `Arrow Down/Right` to find the next match.
*   `Arrow Keys (Up, Down, Left, Right)`: Move the cursor.
*   `Page Up / Page Down`: Scroll the view up or down by a full screen height.
*   `Home`: Move the cursor to the beginning of the current line.
*   `End`: Move the cursor to the end of the current line.
*   `Backspace` / `Ctrl-H`: Delete the character before the cursor.
*   `Enter`: Insert a new line at the cursor position.
*   `Esc`: Can be used to cancel prompts (like Save As or Search).
*   `Ctrl-L`: Refresh the screen (standard terminal behavior often handled by this).

## Development

**Build and Run:**

The included `Makefile` provides a simple way to build and run the editor:

```bash
make run
```

This command compiles the code into a binary named `kilo` in the current directory and then executes it.

**Build Only:**

```bash
go build .
```

## Project Structure

*   `kilo.go`: Contains the entire source code for the editor, including terminal handling, editor state (`EditorConfig`), row management (`eRow`), input processing, rendering, file I/O, and syntax highlighting logic.
*   `go.mod`, `go.sum`: Go module files defining dependencies (`golang.org/x/sys`).
*   `Makefile`: Simple commands for building and running.
*   `.gitignore`: Specifies files to be ignored by Git (like the compiled binary).

## Syntax Highlighting

The current syntax highlighting implementation is basic:

*   It identifies sequences of digits as numbers (`HL_NUMBER`).
*   It temporarily highlights search matches (`HL_MATCH`).
*   It uses a simple `HL_DB` (Highlight Database) to associate file extensions (`.c`, `.h`, `.cpp`, `.go`) with highlighting flags. Currently, this only enables number highlighting for these types.
*   Colors are defined using ANSI escape codes.

This system could be expanded significantly to support comments, strings, keywords, and more complex language structures.

## Future Improvements / TODOs

*   [ ] Add comprehensive unit tests.
*   [ ] Implement Copy/Paste functionality.
*   [ ] Add Undo/Redo capabilities.
*   [ ] Enhance syntax highlighting (keywords, strings, comments, more languages).
*   [ ] Implement configuration file support (e.g., for tab size, key bindings).
*   [ ] Add line numbers display.
*   [ ] Improve error handling and reporting.
*   [ ] Investigate mouse support.
*   [ ] Support for multiple buffers/tabs/windows.
*   [ ] Refactor code into smaller, more manageable packages (e.g., `terminal`, `editor`, `row`).

## Acknowledgements

*   **Salvatore Sanfilippo (antirez)** for creating the original Kilo editor and the tutorial.
*   The **Snaptoken** website for the detailed [Build Your Own Text Editor](https://viewsourcecode.org/snaptoken/kilo/) tutorial which served as the primary reference.
*   The Go Authors for the Go programming language and the `golang.org/x/sys` package.

