package recording

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pion/webrtc/v4"
)

// Mux invokes ffmpeg to combine recorded IVF (VP8) and Ogg (Opus) tracks
// into a single MP4 with H.264 video grid + AAC audio mix.
//
// Layout: every video gets scaled to 640x360, hstacked into a horizontal
// strip. For >4 publishers this gets very wide; acceptable as v1.
// Every audio track is amix'd.
//
// Tracks whose StartOffsetMs > 0 are aligned with -itsoffset so they appear
// at the correct moment in the final file.
// scalepad returns an ffmpeg filter chain that scales to WxH while preserving
// aspect ratio, padding with black bars to fill the remaining space.
func scalepad(w, h int) string {
	return fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black,setsar=1",
		w, h, w, h,
	)
}

func Mux(ctx context.Context, ffmpegPath string, tracks []TrackEntry, outPath string, log *slog.Logger) error {
	var videos, audios []TrackEntry
	for _, t := range tracks {
		switch t.Kind {
		case webrtc.RTPCodecTypeVideo:
			videos = append(videos, t)
		case webrtc.RTPCodecTypeAudio:
			audios = append(audios, t)
		}
	}

	if len(videos) == 0 && len(audios) == 0 {
		return fmt.Errorf("no tracks to mux")
	}

	args := []string{"-y", "-loglevel", "warning"}

	// Inputs: videos first, then audios. Add -itsoffset for late starts.
	for _, t := range videos {
		if t.StartOffsetMs > 0 {
			args = append(args, "-itsoffset", fmt.Sprintf("%.3f", float64(t.StartOffsetMs)/1000.0))
		}
		args = append(args, "-i", t.FilePath)
	}
	for _, t := range audios {
		if t.StartOffsetMs > 0 {
			args = append(args, "-itsoffset", fmt.Sprintf("%.3f", float64(t.StartOffsetMs)/1000.0))
		}
		args = append(args, "-i", t.FilePath)
	}

	var filterParts []string

	// Video filter: scale each + hstack.
	var vLabel string
	switch len(videos) {
	case 0:
		// no video — generate black 1x1 placeholder
		args = append(args, "-f", "lavfi", "-i", "color=c=black:s=320x180:r=15")
		vLabel = fmt.Sprintf("[%d:v]", len(videos)+len(audios))
	case 1:
		filterParts = append(filterParts, "[0:v]"+scalepad(640, 360)+"[vout]")
		vLabel = "[vout]"
	default:
		parts := make([]string, 0, len(videos))
		for i := range videos {
			parts = append(parts, fmt.Sprintf("[%d:v]%s[v%d]", i, scalepad(640, 360), i))
		}
		stackInputs := make([]string, 0, len(videos))
		for i := range videos {
			stackInputs = append(stackInputs, fmt.Sprintf("[v%d]", i))
		}
		parts = append(parts, fmt.Sprintf("%shstack=inputs=%d[vout]", strings.Join(stackInputs, ""), len(videos)))
		filterParts = append(filterParts, strings.Join(parts, ";"))
		vLabel = "[vout]"
	}

	// Audio filter: amix.
	var aLabel string
	switch len(audios) {
	case 0:
		args = append(args, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
		aLabel = fmt.Sprintf("[%d:a]", len(videos)+len(audios))
	case 1:
		filterParts = append(filterParts, fmt.Sprintf("[%d:a]anull[aout]", len(videos)))
		aLabel = "[aout]"
	default:
		parts := make([]string, 0, len(audios))
		for i := range audios {
			parts = append(parts, fmt.Sprintf("[%d:a]", len(videos)+i))
		}
		filterParts = append(filterParts,
			fmt.Sprintf("%samix=inputs=%d:duration=longest:dropout_transition=0[aout]",
				strings.Join(parts, ""), len(audios)))
		aLabel = "[aout]"
	}

	args = append(args, "-filter_complex", strings.Join(filterParts, ";"))
	args = append(args, "-map", vLabel, "-map", aLabel)
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		outPath,
	)

	log.Info("ffmpeg mux", "out", filepath.Base(outPath), "videos", len(videos), "audios", len(audios))
	log.Debug("ffmpeg args", "args", args)

	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	cmd.Stdout = nil
	stderr := &lineLogger{log: log}
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w (last: %s)", err, stderr.Last())
	}
	return nil
}

type lineLogger struct {
	log  *slog.Logger
	last string
}

func (l *lineLogger) Write(p []byte) (int, error) {
	s := strings.TrimRight(string(p), "\n")
	if s != "" {
		l.last = s
		l.log.Debug("ffmpeg", "msg", s)
	}
	return len(p), nil
}

func (l *lineLogger) Last() string { return l.last }
