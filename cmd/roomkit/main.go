package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/dixydo/roomkit/internal/config"
	"github.com/dixydo/roomkit/internal/recording"
	"github.com/dixydo/roomkit/internal/server"
	roomturn "github.com/dixydo/roomkit/internal/turn"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	var testS3 bool
	flag.BoolVar(&testS3, "s3-test", false, "upload a small random PNG to S3 and exit (diagnostic)")

	cfg := config.Load()
	log := newLogger(cfg.LogLevel)

	log.Info("roomkit", "version", version)

	if testS3 {
		if err := runS3Test(cfg.Recording, log); err != nil {
			log.Error("s3 test FAILED", "err", err)
			os.Exit(1)
		}
		return
	}

	if cfg.TURN.PublicIP != "" && cfg.TURN.Secret == "" {
		log.Error("turn-public-ip set without turn-secret; refusing to start")
		os.Exit(1)
	}

	var turnSrv *roomturn.Server
	if cfg.TURN.Enabled() {
		ts, err := roomturn.New(roomturn.Config{
			PublicIP: cfg.TURN.PublicIP,
			Port:     cfg.TURN.Port,
			Secret:   cfg.TURN.Secret,
			Realm:    cfg.TURN.Realm,
			MinPort:  cfg.TURN.MinPort,
			MaxPort:  cfg.TURN.MaxPort,
		}, log)
		if err != nil {
			log.Error("init turn", "err", err)
			os.Exit(1)
		}
		turnSrv = ts
	} else {
		log.Info("turn disabled (set ROOMKIT_TURN_PUBLIC_IP and ROOMKIT_TURN_SECRET to enable)")
	}
	defer func() {
		if turnSrv != nil {
			_ = turnSrv.Close()
		}
	}()

	log.Info("node", "id", cfg.Node.NodeID, "public-url", orDash(cfg.Node.PublicURL))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(ctx, cfg, log)
	if err != nil {
		log.Error("init server", "err", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server stopped with error", "err", err)
		os.Exit(1)
	}
	log.Info("bye")
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

// runS3Test exercises every layer in order so we know exactly where it dies:
//
//  1. ListBuckets — credentials valid at all?
//  2. HeadBucket  — bucket exists in the configured region?
//  3. PutObject   — credentials allowed to write to this bucket?
//  4. HTTP GET    — uploaded object publicly readable?
func runS3Test(cfg config.RecordingConfig, log *slog.Logger) error {
	if !cfg.S3Enabled() {
		return errors.New("S3 not configured (need ROOMKIT_S3_BUCKET + ACCESS_KEY + SECRET_KEY)")
	}

	log.Info("s3 test config",
		"endpoint", orDash(cfg.S3Endpoint),
		"region", cfg.S3Region,
		"bucket", cfg.S3Bucket,
		"object-acl", orDash(cfg.S3ObjectACL),
		"public-base", orDash(cfg.S3PublicBase),
		"access-key-prefix", safePrefix(cfg.S3AccessKey, 4),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKey, cfg.S3SecretKey, "",
		)),
	)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
	})

	// Step 1: ListBuckets (informational — scoped keys may get 403 here, that's ok)
	fmt.Println("--- 1. ListBuckets (verify creds; optional) ---")
	if out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
		fmt.Println("SKIP. Key may be scoped to a single bucket (this is fine).")
		describeError(log, err)
	} else {
		names := make([]string, 0, len(out.Buckets))
		for _, b := range out.Buckets {
			names = append(names, aws.ToString(b.Name))
		}
		fmt.Println("OK. Buckets visible to this key:", names)
		found := false
		for _, n := range names {
			if n == cfg.S3Bucket {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("WARN: configured bucket %q not in the list above.\n", cfg.S3Bucket)
		}
	}

	// Step 2: HeadBucket
	fmt.Println("--- 2. HeadBucket (bucket reachable + region match) ---")
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(cfg.S3Bucket)}); err != nil {
		describeError(log, err)
		return fmt.Errorf("HeadBucket failed: %w", err)
	}
	fmt.Println("OK.")

	// Step 3: PutObject
	fmt.Println("--- 3. PutObject (write permission) ---")
	data, err := randomPNG()
	if err != nil {
		return fmt.Errorf("encode png: %w", err)
	}
	key := fmt.Sprintf("s3-tests/%s.png", time.Now().UTC().Format("20060102T150405Z"))

	s3client, err := recording.NewS3(ctx, cfg)
	if err != nil {
		return fmt.Errorf("init s3 wrapper: %w", err)
	}
	url, err := s3client.UploadBytes(ctx, key, data, "image/png")
	if err != nil {
		describeError(log, err)
		return fmt.Errorf("PutObject failed: %w", err)
	}
	fmt.Println("OK. Uploaded:", url)

	// Step 4: Public GET
	fmt.Println("--- 4. HTTP GET (public readability) ---")
	httpCtx, httpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer httpCancel()
	req, _ := http.NewRequestWithContext(httpCtx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("HTTP error:", err)
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))

	if resp.StatusCode == 200 {
		fmt.Println("OK. End-to-end works. Open the URL above in a browser.")
		return nil
	}

	fmt.Printf("FAIL: GET returned %d.\nBody preview: %s\n", resp.StatusCode, string(body))
	fmt.Println("HINT: object uploaded but not publicly readable.")
	fmt.Println("      Make Space-level public read in DO Spaces Console")
	fmt.Println("      OR set ROOMKIT_S3_OBJECT_ACL=public-read and retry.")
	return nil
}

func describeError(log *slog.Logger, err error) {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		log.Error("S3 API error",
			"code", apiErr.ErrorCode(),
			"message", apiErr.ErrorMessage(),
			"fault", apiErr.ErrorFault().String(),
		)
	} else {
		log.Error("non-API error", "err", err)
	}
}

func randomPNG() ([]byte, error) {
	rgb := make([]byte, 3)
	if _, err := rand.Read(rgb); err != nil {
		return nil, err
	}
	c := color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func safePrefix(s string, n int) string {
	if s == "" {
		return "-"
	}
	if len(s) <= n {
		return s
	}
	return s[:n] + "***"
}
