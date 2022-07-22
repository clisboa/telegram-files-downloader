# telegram-files-downloader bot

## A Telegram bot for downloading files.
Just forward a file/photo/video to the bot and it will download them specified location.
E.g. batched photo downloads from tg channels to NAS.

**Important: Telegram bot API has limit of download size - 20MB.**

## Bot commands:
- `/help` - show help
- `/cd [-r] <path>` - change working directory (-r: reset to initial working dir)
- `/pwd` - print working directory
- `/ls` - list files in current working directory
- `/stats` - print statistics

## How to build locally:
```bash
  go mod download
  go build
```

## How to run locally from source code:
```bash
  export TELEGRAM_TOKEN="<bot token>"
  export TELEGRAM_CHATID="<chat id>"
  go run ./main.go
```

## How to build docker container:
```bash
  docker build -t telegram-files-downloader .
```

## How to run in docker container:
```bash
docker run -d \
  --name=telegram-files-downloader \
  -e TELEGRAM_TOKEN="<bot token>" \
  -e TELEGRAM_CHATID="<chat id>" \
  -v <target folder on host>:/data \
  -w /data \
  --restart unless-stopped \
  telegram-files-downloader
```

Where:
- `<bot token>` - bot token from @BotFather. See [instructions](https://core.telegram.org/bots#6-botfather).
- `<chat id>` - chat id where to send messages for downloads. It is optional. Use `curl -X GET https://api.telegram.org/bot<YOUR_API_TOKEN>/getUpdates` after sending a message get chat id.
- `<target folder on host>` - a destination folder where files will be saved to. Use `/cd` will automatically create subfolder inside it.
