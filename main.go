package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/go-telegram/ui/keyboard/inline"
)

var errorLog = log.New(os.Stderr, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

func toString(inf any) string {
	res, err := json.Marshal(inf)
	if err != nil {
		return fmt.Sprintf("%v", inf)
	}
	return string(res)
}

func init() {
	if err := initDb(); err != nil {
		panic(err)
	}

}

func main() {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		// bot.WithDebug(),
		bot.WithDefaultHandler(defaultHandler),
		bot.WithCallbackQueryDataHandler("deleteListElement", bot.MatchTypePrefix, onListElementClick),
		bot.WithCallbackQueryDataHandler("undoDeleteListElement", bot.MatchTypePrefix, onListUndoDelete),
		bot.WithCallbackQueryDataHandler("redrawList", bot.MatchTypePrefix, listRedraw),
		bot.WithCallbackQueryDataHandler("switchList", bot.MatchTypePrefix, listSwitch),
	}

	b, err := bot.New(os.Getenv("MISTER_LISTER_TOKEN"), opts...)
	if nil != err {
		// panics for the sake of simplicity.
		// you should handle this error properly in your code.
		panic(err)
	}
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypeExact, helpHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/show", bot.MatchTypeExact, drawListItemsHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/share", bot.MatchTypePrefix, shareHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/list", bot.MatchTypeExact, selectListHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/new", bot.MatchTypePrefix, newListHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/me", bot.MatchTypeExact, meHandler)
	b.Start(ctx)
}

func helpHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	sendMessage(ctx, b, update.Message.Chat.ID, `/help
Посмотреть доступные команды

/show
Показать активный список

/new *Название*
Создать новый список

/list
Меню выбор активного списка

/share *id*
Расшарить список с указанным id

/me
Показать мой ID

Связаться с автором: @uscr0`)
}

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	sendMessage(ctx, b, update.Message.Chat.ID, `Привет\!
Это бот для работы со списками\. Покупок или дел\.
Списки можно расхарить с другим пользователем бота\.

Для начала нужно создать свой первый список командой\: 
/new *Название*

Вот все команды которые поддерживает бот\:`)
	helpHandler(ctx, b, update)
}

func meHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	sendMessage(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Ваш ID: %d", update.Message.Chat.ID))
}

func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if strings.HasPrefix(update.Message.Text, "/") {
		sendMessage(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Команда не распознана"))
		helpHandler(ctx, b, update)
		return
	}
	userId := update.Message.Chat.ID
	err := addItem(userId, update.Message.Text)
	if err != nil {
		errorLog.Printf("[defaultHandler] User: %d, Can't add item: %s", userId, err)
		sendMessage(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Ошибка добавления"))
		helpHandler(ctx, b, update)
		return
	}
	sendMessage(ctx, b, userId, fmt.Sprintf("Добавили %s", update.Message.Text))
	drawListItemsHandler(ctx, b, update)
}

func shareHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID
	words := strings.Fields(update.Message.Text)
	if len(words) < 2 {
		helpHandler(ctx, b, update)
		return
	}
	var selectedList List
	selectedList, err := getSelectedList(userId)
	if err != nil {
		errorLog.Printf("[shareHandler] User %d. Error when getSelectedList: %s", userId, err)
		sendMessage(ctx, b, userId, "Неизвестная ошибка")
		return
	}
	sharedWithId, err := strconv.ParseInt(words[1], 0, 64)
	if err != nil {
		errorLog.Printf("[shareHandler] User %d. Error parse id to int: %s", userId, err)
		sendMessage(ctx, b, userId, fmt.Sprintf(`Вместо %s указать ID пользователя с которым вы хотите расшарить список '%s'
ID можно узнать командой /me`, words[1], selectedList.Name))
		return
	}
	if sharedWithId == userId {
		sendMessage(ctx, b, userId, fmt.Sprintf(`Вы указали свой ID
Нужно указать ID пользователя с которым вы хотите расшарить список '%s'
Свой ID пользователь может узнать отправив команду /me этому боту`, selectedList.Name))
		return
	}
	err = addOwner(sharedWithId, selectedList.ID)
	if err != nil {
		errorLog.Printf("[shareHandler] User %d. Error when add owner: %s", userId, err)
		sendMessage(ctx, b, userId, "Неизвестная ошибка")
		return
	}

	sendMessage(ctx, b, userId, "Готово")
}

func sendMessage(ctx context.Context, b *bot.Bot, chatId int64, text string) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatId,
		Text:      text,
		ParseMode: models.ParseModeMarkdown,
	})
}

func sendInlineKeyboard(ctx context.Context, b *bot.Bot, chatId int64, text string, kb *inline.Keyboard) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatId,
		Text:        text,
		ReplyMarkup: kb,
	})
}

func sendMarkupKeyboard(ctx context.Context, b *bot.Bot, chatId int64, text string, kb *models.InlineKeyboardMarkup) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatId,
		Text:        text,
		ReplyMarkup: kb,
	})
}

func answerCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		ShowAlert:       false,
	})
}

// 2023/11/19 13:50:28 [TGBOT] [DEBUG] response from 'https://api.telegram.org/bot6674500517:AAEW8XfU4Utw7449BqrPoW48hr-oM3-W3CM/getUpdates' with payload '{"ok":true,"result":[{"update_id":33415153,
// "message":{"message_id":1312,"from":
// {"id":50590644,"is_bot":false,"first_name":"Denis","last_name":"Nazarov","username":"UsCr0","language_code":"ru","is_premium":true},
// "chat":{"id":50590644,"first_name":"Denis","last_name":"Nazarov","username":"UsCr0","type":"private"},
// "date":1700391028,"text":"/start","entities":[{"offset":0,"length":6,"type":"bot_command"}]}}]}'
