package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
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
	cfg.InitialWorkingDir = mustGetWd()
	log.Println("Working directory:", cfg.InitialWorkingDir)

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

func mustGetWd() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return wd
}

func handleHelp(c tele.Context) error {
	msg := "This is a bot for downloading attachments.\n"
	msg += fmt.Sprintf("Chat ID: %d\nCommands:\n", c.Chat().ID)
	msg += "/help - show this help\n"
	msg += "/cd [-r] <path> - change working directory (-r: reset to initial working dir)\n"
	msg += "/pwd - print working directory\n"
	msg += "/ls - list files in current working directory\n"
	msg += "/stats - print statistics\n"
	return c.Send(msg)
}

func handlePwd(c tele.Context) error {
	return c.Reply(mustGetWd())
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

func handleLs(c tele.Context) error {
	wd := mustGetWd()
	files, err := ioutil.ReadDir(wd)
	if err != nil {
		return err
	}
	msg := "Files in " + wd + ":\n"
	for _, file := range files {
		t := '-'
		if file.IsDir() {
			t = 'd'
		}
		s := fmt.Sprintf("%c %-50s: %s\n",
			t, file.Name(), humanReadableSize(file.Size()))
		if len(s)+len(msg) >= 400 {
			c.Reply(msg)
			msg = ""
		}
		msg += s
	}
	return c.Reply(msg)
}

func handleCd(c tele.Context) error {
	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Usage: /cd [-r] <path>")
	}
	wd := args[0]
	if wd == "-r" {
		wd = cfg.InitialWorkingDir
	}

	if filepath.IsAbs(wd) && !strings.HasPrefix(wd, cfg.InitialWorkingDir) {
		logEverywhere(c, "Path is not relative to initial working dir")
		return errorOutside
	}

	err := os.MkdirAll(wd, 0755)
	if err != nil {
		logEverywhere(c, "MkdirAll: %s", err.Error())
		return err
	}

	err = os.Chdir(wd)
	if err != nil {
		logEverywhere(c, "Chdir: %s", err.Error())
		return err
	}

	log.Printf("Working directory changed to %s", wd)
	return c.Reply("done!")
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

	fpath := filepath.Join(mustGetWd(), fname)
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

func handleOnPhoto(c tele.Context) error {
	p := c.Message().Photo
	go downloadFile(c, p.MediaFile(), p.UniqueID+".jpg")
	return nil
}

func handleOnVideo(c tele.Context) error {
	v := c.Message().Video
	if v.MIME != "video/mp4" {
		logEverywhere(c, "Unsupported video format: %s, wants 'video/mp4'", v.MIME)
		// download anyway
	}
	go downloadFile(c, v.MediaFile(), v.UniqueID+".mp4")
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
	b.Handle("/ls", handleLs)
	b.Handle("/pwd", handlePwd)
	b.Handle("/cd", handleCd)
	b.Handle("/stats", handleStats)

	b.Handle(tele.OnPhoto, handleOnPhoto)
	b.Handle(tele.OnDocument, handleOnDocument)
	b.Handle(tele.OnVideo, handleOnVideo)

	b.Start()
}
