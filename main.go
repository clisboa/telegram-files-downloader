package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	tele "gopkg.in/telebot.v4"
	"gopkg.in/telebot.v4/middleware"
)

type Cfg struct {
	InitialWorkingDir string
	TelegramToken     string
	WhitelistedChatID int64
}

type Stats struct {
	startTime        time.Time
	DowloadsOk       uint32
	DownloadsErr     uint32
	DownloadsPending uint32
}

var errorOutside = errors.New("outside initial working dir")

var cfg Cfg
var stats Stats

func initCfg() {
	cfg.InitialWorkingDir = os.Getenv("TELEGRAM_DEST")
	if cfg.InitialWorkingDir == "" {
		log.Fatal("TELEGRAM_DEST is not set")
	}
	log.Println("Working directory:", cfg.InitialWorkingDir)
	os.Setenv("TELEGRAM_DEST", "")

	cfg.TelegramToken = os.Getenv("TELEGRAM_TOKEN")
	if cfg.TelegramToken == "" {
		log.Fatal("TELEGRAM_TOKEN is not set")
	}
	os.Setenv("TELEGRAM_TOKEN", "")

	chatId := os.Getenv("TELEGRAM_CHATID")
	var err error
	if chatId != "" {
		cfg.WhitelistedChatID, err = strconv.ParseInt(chatId, 10, 64)
		if err != nil {
			log.Fatalf("TELEGRAM_CHATID is not a valid number: err=%s",
				err.Error())
		}
		os.Setenv("TELEGRAM_CHATID", "")
	}
}

func handleHelp(c tele.Context) error {
	msg := "This is a bot for downloading attachments.\n"
	msg += fmt.Sprintf("Chat ID: %d\nCommands:\n", c.Chat().ID)
	msg += "/help - show this help\n"
	msg += "/stats - print statistics\n"
	return c.Send(msg)
}

func humanReadableSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%d KB", size/1024)
	}

	if size < 1024*1024*1024 {
		return fmt.Sprintf("%d MB", size/1024/1024)
	}
	return fmt.Sprintf("%d GB", size/1024/1024/1024)
}

func handleStats(c tele.Context) error {
	ok := atomic.LoadUint32(&stats.DowloadsOk)
	fail := atomic.LoadUint32(&stats.DownloadsErr)
	pending := atomic.LoadUint32(&stats.DownloadsPending)
	logEverywhere(c, "Stats:\nUptime: %s\nDownloads : %d/%d (pending: %d)",
		time.Since(stats.startTime), ok, ok+fail, pending)
	return nil
}

func logEverywhere(c tele.Context, format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	log.Println(s)
	c.Reply(s)
}

func downloadFile(c tele.Context, f *tele.File, fname string) {
	atomic.AddUint32(&stats.DownloadsPending, 1)
	downloadFileInternal(c, f, fname)
	pending := atomic.AddUint32(&stats.DownloadsPending, ^uint32(0))
	if pending == 0 {
		logEverywhere(c, "All downloads finished")
	} else if pending%5 == 0 {
		logEverywhere(c, "Done. Pending downloads: %d", pending)
	}
}

func downloadFileInternal(c tele.Context, f *tele.File, fname string) {
	log.Printf("Enqueued: %s\n", fname)
	logEverywhere(c, "Enqueued: %s\n", fname)

	fpath := filepath.Join(cfg.InitialWorkingDir, fname)
	tmp := fpath + ".tmp"

	if err := c.Bot().Download(f, tmp); err != nil {
		logEverywhere(c, "Error: Download: %s", err.Error())
		atomic.AddUint32(&stats.DownloadsErr, 1)
		return
	}

	if err := os.Rename(tmp, fpath); err != nil {
		logEverywhere(c, "Error: Rename: %s", err.Error())
		atomic.AddUint32(&stats.DownloadsErr, 1)
		return
	}
	atomic.AddUint32(&stats.DowloadsOk, 1)
}

func handleOnDocument(c tele.Context) error {
	doc := c.Message().Document
	fname := doc.FileName
	if fname == "" {
		log.Printf("Document without filename: %s", doc.UniqueID)
		fname = doc.UniqueID
	}
	go downloadFile(c, doc.MediaFile(), fname)
	return nil
}

func main() {
	stats.startTime = time.Now()

	initCfg()

	pref := tele.Settings{
		Token:  cfg.TelegramToken,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	if cfg.WhitelistedChatID != 0 {
		b.Use(middleware.Whitelist(cfg.WhitelistedChatID))
		log.Printf("Whitelisted chat ID: %d", cfg.WhitelistedChatID)
	}

	b.Handle("/help", handleHelp)
	b.Handle("/stats", handleStats)

	b.Handle(tele.OnDocument, handleOnDocument)

	b.Start()
}
