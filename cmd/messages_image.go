package cmd

import (
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/arikawa/v3/discord"
)

func isImageAttachment(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp"
}

func (mt *MessagesText) downloadImage(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (mt *MessagesText) isKittyProtocolSupported() bool {
	term := os.Getenv("TERM")
	if term == "xterm-kitty" {
		return true
	}

	if os.Getenv("GHOSTTY") != "" {
		return true
	}

	return false
}

func (mt *MessagesText) renderAttachmentImages(w io.Writer, m discord.Message) {
	for _, a := range m.Attachments {
		if isImageAttachment(a.Filename) {
			mt.displayImage(w, a.URL, int(a.Width), int(a.Height))
		}
	}
}

func (mt *MessagesText) renderEmbedImages(w io.Writer, m discord.Message) {
	for _, embed := range m.Embeds {
		if embed.Image != nil {
			mt.displayImage(w, embed.Image.URL, int(embed.Image.Width), int(embed.Image.Height))
		}
		if embed.Thumbnail != nil {
			mt.displayImage(w, embed.Thumbnail.URL, int(embed.Thumbnail.Width), int(embed.Thumbnail.Height))
		}
	}
}

func (mt *MessagesText) displayImage(w io.Writer, url string, width, height int) {
	img, err := mt.downloadImage(url)
	if err != nil {
		slog.Error("failed to download image", "err", err, "url", url)
		return
	}

	maxWidth := 40
	maxHeight := 10

	scaledWidth, scaledHeight := calculateImageDimensions(width, height, maxWidth, maxHeight)

	mt.writeChunkedImage(w, img, imgID, scaledWidth, scaledHeight)
}

func calculateImageDimensions(width, height, maxWidth, maxHeight int) (int, int) {
	ratio := float64(width) / float64(height)

	if width > maxWidth {
		width = maxWidth
		height = int(float64(height) / ratio)
	}

	if height > maxHeight {
		height = maxHeight
		height = int(float64(height) * ratio)
	}

	return height, width
}

func generateImageID(url string) string {
	hasher := fnv.New32a()
	hasher.Write([]byte(url))
	hash := hasher.Sum32() & 0x00FFFFFF

	return fmt.Sprintf("img_%x", hash)
}

func (mt *MessagesText) writeChunkedImage(w io.Writer, imgData []byte, id string, width, height int) {
	encoded := base64.StdEncoding.EncodeToString(imgData)
	os.WriteFile(id+".log", []byte(encoded), 0644)

	chunkSize := 4096
	pos := 0

	for pos < len(encoded) {
		_, err := mt.createBody(w, "\033_G", false, true)
		if err != nil {
			slog.Error("failed to write image start sequence", "err", err)
			return
		}

		if pos == 0 {
			_, err = fmt.Fprintf(w, "a=T,f=100,w=%d,h=%d,", width, height)
			if err != nil {
				slog.Error("failed to write image parameters", "err", err)
				return
			}
		}

		var chunk string
		if pos+chunkSize < len(encoded) {
			chunk = encoded[pos : pos+chunkSize]
			pos += chunkSize
			_, err = fmt.Fprint(w, "m=1")
		} else {
			chunk = encoded[pos:]
			pos = len(encoded)
		}

		if err != nil {
			slog.Error("failed to write more flag", "err", err)
			return
		}

		if len(chunk) > 0 {
			_, err = fmt.Fprintf(w, ";%s", chunk)
			if err != nil {
				slog.Error("failed to write image chunk", "err", err)
				return
			}
		}

		_, err = fmt.Fprint(w, "\033\\")
		if err != nil {
			slog.Error("failed to write image end sequence", "err", err)
			return
		}

	}
}
