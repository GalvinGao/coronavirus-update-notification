package main

import (
	"bytes"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const TelegramBotToken = "YOUR_BOT_TOKEN_HERE"
const TelegramChatId int64 = YOUR_CHANNEL_OR_PERSONAL_CHAT_ID_HERE
const TelegramPersonalChatId int64 = YOUR_PERSONAL_CHAT_ID_HERE // intended to send error log directly to your telegram account
const ApiURL = "https://coronavirus.1point3acres.com"

type TelegramErrorLogger struct {}

func (l TelegramErrorLogger) Write(data []byte) (n int, err error) {
	m := tgbotapi.NewMessage(TelegramPersonalChatId, fmt.Sprintf("push error: %v", string(data)))
	Bot.Send(m)
	return 0, nil
}

var telegramErrorLogger = TelegramErrorLogger{}

var client = http.Client{
	Timeout: time.Minute,
}
var MultiLogDest = io.MultiWriter(os.Stdout, telegramErrorLogger)
var Log = log.New(os.Stdout, "[ncov-change-notif] ", log.LstdFlags)
var ErrorLog = log.New(MultiLogDest, "[ERROR] [ncov-change-notif] ", log.LstdFlags)

// Category -> Key:Value
var Cache = map[string]map[string]int{}

var Bot *tgbotapi.BotAPI


type Diff struct {
	Current int
	Diff string
}

func parse(r io.Reader) string {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		ErrorLog.Printf("failed to init goquery doc: %v", err)
	}

	diff := map[string]map[string]Diff{}

	needUpdate := false
	firstRun := false

	tags := doc.Find("#stat").Find(".tag")
	tags.Each(func(i int, selection *goquery.Selection) {
		category := selection.Find("strong").Text()
		category = strings.ReplaceAll(category, "确诊", "")
		selection.Find("dl").Each(func(i int, selection *goquery.Selection) {
			term := selection.Find("dt").Text()
			def := selection.Find("dd").Text()
			o := Cache[category][term]
			defInt, err := strconv.Atoi(def)
			if err != nil {
				ErrorLog.Printf("cannot conv %v to int: %v", def, err)
			}

			if o != defInt {
				needUpdate = true
				if _, ok := Cache[category]; !ok {
					Cache[category] = map[string]int{}
					needUpdate = false
					firstRun = true
				}
				Cache[category][term] = defInt

				if _, ok := diff[category]; !ok {
					diff[category] = map[string]Diff{}
				}

				diffValue := defInt - o
				var diffText string
				if diffValue > 0 {
					diffText = fmt.Sprintf("增加了%d", diffValue)
				} else {
					diffText = fmt.Sprintf("减少了%d", -diffValue)
				}
				diff[category][term] = Diff{
					Current: defInt,
					Diff:    diffText,
				}
			}
		})
	})

	tmpl := template.Must(template.New("TgUpdate").Parse(`**概览**
{{range $k, $v := .Diff}}{{range $kk, $vv := $v}}*{{$k}}*的{{$kk}}人数*{{$vv.Diff}}* ({{$vv.Current}})；
{{end}}{{end}}
**现总计**
{{range $k, $v := .Cache}}  *{{$k}}*{{range $kk, $vv := $v}}
    — {{$kk}}：*{{$vv}}*{{end}}
{{end}}
`))
	s := bytes.NewBufferString("")
	err = tmpl.Execute(s, struct {
		Cache map[string]map[string]int
		Diff map[string]map[string]Diff
	}{
		Cache: Cache,
		Diff: diff,
	})
	if err != nil {
		ErrorLog.Printf("failed to exec tmpl: %v", err)
	}

	if needUpdate && !firstRun {
		Log.Printf("have change; sending update.")
		return s.String()
	} else {
		Log.Printf("no change.")
		return ""
	}
}

func update() {
	Log.Printf("updating...")
	resp, err := client.Get(ApiURL)
	if err != nil {
		ErrorLog.Printf("failed to fetch api: %v", err)
		return
	}
	s := parse(resp.Body)
	fmt.Println(s)
	if s != "" {
		m := tgbotapi.NewMessage(TelegramChatId, s)
		m.ParseMode = tgbotapi.ModeMarkdown
		_, err := Bot.Send(m)
		if err != nil {
			ErrorLog.Printf("send message failed: %v", err)
		}
	}
}

func main() {
	var err error
	Bot, err = tgbotapi.NewBotAPIWithClient(TelegramBotToken, &client)
	if err != nil {
		ErrorLog.Printf("failed to init bot: %v", err)
	}
	update()
	t := time.NewTicker(time.Minute * 5)
	for {
		select {
		case <-t.C:
			update()
		}
	}
}

