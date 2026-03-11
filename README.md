# gnotes 🚀

A high-performance, secure, and lightweight note-taking system built with **Go** and **Podman**. 

`gnotes` is designed to be a "no-bloat" alternative for managing large documents, supporting both Markdown and Rich Text with seamless switching.

## ✨ Features (Planned)
- **Hybrid Editor:** Switch between Markdown and WYSIWYG.
- **High Efficiency:** Powered by Go's standard library and SQLite.
- **Secure Sandbox:** Podman-integrated execution for C/Go code snippets.
- **Large Asset Support:** Efficient streaming of Images and PDFs.
- **Zero Bloat:** Minimal dependencies and fast cold-start times.

## 🛠 Tech Stack
- **Backend:** Go 1.2x+
- **Database:** SQLite (Modernc pure-Go driver)
- **Containerization:** Podman (Rootless)
- **Frontend:** Vanilla JS / React (Headless Tiptap)

## 🚀 Getting Started

### Prerequisites
- Go installed (on Arco Linux: `sudo pacman -S go`)
- Podman (for sandboxing)
- `mkcert` (for local HTTPS development)

### Running Locally
1. Clone the repository:
   ```bash
   git clone [https://github.com/yourusername/gnotes.git](https://github.com/yourusername/gnotes.git)
   cd gnotes