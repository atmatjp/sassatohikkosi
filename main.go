package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

//機密情報は.envに
//BOT_TOKEN=
//SOURCE_CHANNEL_ID=
//TARGET_FORUM_ID=
//WEBHOOK_URL=
//WEBHOOK_NAME=

// 名前の決定ロジック
func getDisplayName(dg *discordgo.Session, guildID string, user *discordgo.User, cache map[string]string) string {
	if name, exists := cache[user.ID]; exists {
		return name
	}

	displayName := user.Username
	member, err := dg.GuildMember(guildID, user.ID)
	if err == nil {
		if member.Nick != "" {
			displayName = member.Nick
		} else if member.User.GlobalName != "" {
			displayName = member.User.GlobalName
		}
	} else {
		if user.GlobalName != "" {
			displayName = user.GlobalName
		}
	}
	cache[user.ID] = displayName
	return displayName
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(".env ファイルの読み込みに失敗しました")
	}

	token := os.Getenv("BOT_TOKEN")
	srcID := os.Getenv("SOURCE_CHANNEL_ID")
	// targetID は Webhook が宛先を知っているため、未使用エラー回避のため削除
	webhookURL := os.Getenv("WEBHOOK_URL")
	webhookName := os.Getenv("WEBHOOK_NAME")

	parts := strings.Split(webhookURL, "/")
	if len(parts) < 2 {
		log.Fatal("WEBHOOK_URL の形式が正しくありません")
	}
	webhookID := parts[len(parts)-2]
	webhookToken := parts[len(parts)-1]

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal(err)
	}

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent | discordgo.IntentsGuildMembers

	if err = dg.Open(); err != nil {
		log.Fatal(err)
	}
	defer dg.Close()

	fmt.Println("移行処理を開始します...")

	srcChannel, _ := dg.Channel(srcID)

	var allMessages []*discordgo.Message
	lastID := ""
	fmt.Println("過去ログを取得中...")
	for {
		msgs, err := dg.ChannelMessages(srcID, 100, lastID, "", "")
		if err != nil || len(msgs) == 0 {
			break
		}
		allMessages = append(allMessages, msgs...)
		lastID = msgs[len(msgs)-1].ID
		fmt.Printf("現在 %d 件取得済み...\n", len(allMessages))
	}

	// 古い順にソート
	for i, j := 0, len(allMessages)-1; i < j; i, j = i+1, j-1 {
		allMessages[i], allMessages[j] = allMessages[j], allMessages[i]
	}

	if len(allMessages) == 0 {
		log.Fatal("移行するメッセージが見つかりませんでした。")
	}

	nameCache := make(map[string]string)

	fmt.Println("スレッドを作成し、転送を開始します...")

	// スレッド作成者模倣 ＆ 1件目の送信
	firstMsg := allMessages[0]
	firstDisplayName := getDisplayName(dg, srcChannel.GuildID, firstMsg.Author, nameCache)

	contentFirst := fmt.Sprintf("[%s] %s", firstMsg.Timestamp.Format("2006/01/02 15:04"), firstMsg.Content)
	for _, a := range firstMsg.Attachments {
		contentFirst += "\n(添付ファイル): " + a.URL
	}

	paramsFirst := &discordgo.WebhookParams{
		Content:    contentFirst,
		Username:   firstDisplayName,
		AvatarURL:  firstMsg.Author.AvatarURL("128"),
		ThreadName: srcChannel.Name,
		Flags:      4096,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	}

	respMsg, err := dg.WebhookExecute(webhookID, webhookToken, true, paramsFirst)
	if err != nil {
		log.Fatal("スレッド作成失敗:", err)
	}
	threadID := respMsg.ChannelID
	fmt.Printf("スレッド作成完了: %s (ID: %s)\n", srcChannel.Name, threadID)

	time.Sleep(2 * time.Second)

	// 2件目以降の送信
	for i := 1; i < len(allMessages); i++ {
		m := allMessages[i]
		fmt.Printf("[%d/%d] 送信中...\n", i+1, len(allMessages))

		displayName := getDisplayName(dg, srcChannel.GuildID, m.Author, nameCache)

		content := fmt.Sprintf("[%s] %s", m.Timestamp.Format("2006/01/02 15:04"), m.Content)
		for _, a := range m.Attachments {
			content += "\n(添付ファイル): " + a.URL
		}

		params := &discordgo.WebhookParams{
			Content:   content,
			Username:  displayName,
			AvatarURL: m.Author.AvatarURL("128"),
			Flags:     4096,
			AllowedMentions: &discordgo.MessageAllowedMentions{
				Parse: []discordgo.AllowedMentionType{},
			},
		}

		if params.Username == "" {
			params.Username = webhookName
		}

		tokenWithThread := webhookToken + "?thread_id=" + threadID
		_, err = dg.WebhookExecute(webhookID, tokenWithThread, false, params)
		if err != nil {
			fmt.Printf("送信失敗 (ID: %s): %v\n", m.ID, err)
		}

		time.Sleep(2 * time.Second)
	}

	fmt.Println("\n移行完了")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}
