package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	videosDir := "Videos"
	entries, err := os.ReadDir(videosDir)
	if err != nil {
		log.Fatal("Cannot read Videos/ directory:", err)
	}

	var videos []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".mp4") {
			videos = append(videos, e.Name())
		}
	}

	if len(videos) == 0 {
		log.Fatal("No .mp4 files found in Videos/")
	}

	mux := http.NewServeMux()

	// Serve video files
	mux.Handle("/videos/", http.StripPrefix("/videos/", http.FileServer(http.Dir(videosDir))))

	// POST endpoint to save a thumbnail
	mux.HandleFunc("/save-thumb", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Filename string `json:"filename"`
			Data     string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Strip data URI prefix
		b64 := payload.Data
		if idx := strings.Index(b64, ","); idx != -1 {
			b64 = b64[idx+1:]
		}

		imgBytes, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			http.Error(w, "bad base64: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Save as .jpg next to the video
		ext := filepath.Ext(payload.Filename)
		thumbName := strings.TrimSuffix(payload.Filename, ext) + ".jpg"
		thumbPath := filepath.Join(videosDir, thumbName)

		if err := os.WriteFile(thumbPath, imgBytes, fs.FileMode(0644)); err != nil {
			http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("Saved thumbnail: %s (%d bytes)", thumbPath, len(imgBytes))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ok: %s", thumbName)
	})

	// Serve the generator page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		videosJSON, _ := json.Marshal(videos)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, pageHTML, string(videosJSON))
	})

	addr := ":9999"
	log.Printf("Thumbnail generator running at http://localhost%s", addr)
	log.Printf("Processing %d videos: %v", len(videos), videos)
	log.Println("Open the URL in your browser. Thumbnails will be generated automatically.")
	log.Fatal(http.ListenAndServe(addr, mux))
}

const pageHTML = `<!DOCTYPE html>
<html>
<head>
<title>Thumbnail Generator</title>
<style>
  body { font-family: sans-serif; background: #111; color: #eee; padding: 20px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 16px; }
  .item { background: #222; border-radius: 8px; padding: 12px; }
  .item img { width: 100%%; border-radius: 4px; }
  .item video { display: none; }
  .item h3 { margin: 8px 0 4px; font-size: 14px; }
  .status { font-size: 12px; color: #888; }
  .done { color: #4f4; }
  #progress { font-size: 18px; margin-bottom: 16px; }
</style>
</head>
<body>
<h1>Seed Thumbnail Generator</h1>
<p id="progress">Starting...</p>
<div class="grid" id="grid"></div>

<script>
const videos = %s;
const grid = document.getElementById('grid');
const progress = document.getElementById('progress');
let completed = 0;

function captureFrame(videoEl) {
  const canvas = document.createElement('canvas');
  canvas.width = 640;
  canvas.height = 360;
  const ctx = canvas.getContext('2d');
  ctx.fillStyle = '#1f1f1f';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const scale = Math.min(canvas.width / videoEl.videoWidth, canvas.height / videoEl.videoHeight);
  const w = videoEl.videoWidth * scale;
  const h = videoEl.videoHeight * scale;
  const x = (canvas.width - w) / 2;
  const y = (canvas.height - h) / 2;
  ctx.drawImage(videoEl, x, y, w, h);
  return canvas.toDataURL('image/jpeg', 0.85);
}

async function processVideo(filename) {
  const item = document.createElement('div');
  item.className = 'item';
  item.innerHTML = '<h3>' + filename + '</h3>' +
    '<video muted></video>' +
    '<img>' +
    '<p class="status">Loading...</p>';
  grid.appendChild(item);

  const videoEl = item.querySelector('video');
  const imgEl = item.querySelector('img');
  const statusEl = item.querySelector('.status');

  return new Promise((resolve) => {
    videoEl.src = '/videos/' + filename;
    videoEl.addEventListener('loadeddata', () => {
      videoEl.currentTime = 1.0;
    });
    videoEl.addEventListener('seeked', async () => {
      const dataUrl = captureFrame(videoEl);
      imgEl.src = dataUrl;
      statusEl.textContent = 'Saving...';

      try {
        const res = await fetch('/save-thumb', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ filename: filename, data: dataUrl })
        });
        const text = await res.text();
        statusEl.textContent = text;
        statusEl.className = 'status done';
      } catch (e) {
        statusEl.textContent = 'Error: ' + e.message;
      }

      completed++;
      progress.textContent = completed + ' / ' + videos.length + ' done';
      if (completed === videos.length) {
        progress.textContent += ' - All thumbnails generated! You can close this page.';
      }
      resolve();
    }, { once: true });
  });
}

(async () => {
  for (const v of videos) {
    await processVideo(v);
  }
})();
</script>
</body>
</html>`
