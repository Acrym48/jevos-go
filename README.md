# jevos-go

Go-клиент для [jevos](https://github.com/feder-cr/jev) — локальной модели, которая отвечает на вопросы о тексте: да/нет, выбор из вариантов и оценка по шкале. Модель возвращает вероятности, а не сгенерированный текст, поэтому разбирать ответ не нужно.

Библиотека неофициальная и использует только стандартную библиотеку Go.

## Возможности

- вопросы да/нет: вероятность «да»;
- выбор из нескольких вариантов: победивший вариант и вероятность каждого;
- оценка по шкале: ожидаемый уровень, его название и вероятности уровней;
- `confidence` для вопросов с выбором и шкалой;
- несколько вопросов об одном тексте в одном запросе (сервер читает текст один раз);
- `context.Context`, таймауты, API-ключ, свой `http.Client`.

## Требования

- Go 1.21+
- запущенный сервер jevos: `./jev serve` (по умолчанию `127.0.0.1:8017`). Как его скачать и запустить, написано в [репозитории jevos](https://github.com/feder-cr/jev#quickstart).

## Установка

```sh
go get github.com/Acrym48/jevos-go
```

## Быстрый старт

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Acrym48/jevos-go"
)

func main() {
	c := jevos.New() // http://127.0.0.1:8017

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p, err := c.Ask(ctx,
		"My invoice shows a charge I do not recognize.",
		"Is this a billing problem?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("P(yes) = %.2f\n", p)
}
```

## Использование

### Несколько вопросов за один запрос

```go
res, err := c.Query(text).
	YesNo("refund", "Should this customer get a refund?").
	Choice("team", "Which team should handle this?", map[string]string{
		"billing":  "payments, refunds",
		"shipping": "deliveries, missing parcels",
		"tech":     "a product that does not work",
	}).
	Score("anger", "How angry is the customer?", "calm", "annoyed", "angry", "furious").
	Do(ctx)
if err != nil {
	log.Fatal(err)
}

refund, _ := res.Get("refund")
team, _ := res.Get("team")
anger, _ := res.Get("anger")

fmt.Println(refund.Noul)                       // P(yes)
fmt.Println(team.Choice, team.Probabilities)   // "shipping", map[...]
fmt.Println(anger.Level(), anger.Score)        // "annoyed", 1.07
fmt.Println(anger.Confidence)                  // если низкая — отдайте случай человеку
```

Имена вопросов (`"refund"`, `"team"`, `"anger"`) произвольные, по ним ответ достаётся через `res.Get`. Повторное имя вернёт ошибку из `Do`.

### Одиночные вопросы

```go
p, err := c.Ask(ctx, text, "Is this spam?")                    // float64, P(yes)
ok, err := c.Yes(ctx, text, "Is this spam?", 0.8)               // bool, P(yes) >= 0.8

a, err := c.AskChoice(ctx, text, "What is this message?", map[string]string{
	"ad":     "реклама: скидки, акции, призывы купить",
	"normal": "обычное сообщение",
})
fmt.Println(a.Choice, a.Probabilities["ad"], a.Confidence)

s, err := c.AskScore(ctx, text, "How angry is the customer?", "calm", "annoyed", "angry", "furious")
fmt.Println(s.Level(), s.Score)
```

### Состояние как объект

`state` может быть строкой или любым значением, которое сериализуется в JSON:

```go
res, err := c.Query(map[string]string{
	"item":             "wireless mouse",
	"customer_message": "The box arrived empty. This is the second time!",
}).YesNo("refund", "Should this customer get a refund?").Do(ctx)
```

### Низкоуровневый вызов

```go
res, err := c.Decide(ctx, text, map[string]jevos.Question{
	"spam": jevos.YesNo("Is this spam?"),
	"kind": jevos.Choice("What is this?", map[string]string{"ad": "реклама", "normal": "обычное"}),
	"mood": jevos.Score("How angry?", "calm", "annoyed", "angry", "furious"),
})
```

## Настройка клиента

```go
c := jevos.New(
	jevos.WithBaseURL("http://192.168.1.10:8017"),
	jevos.WithAPIKey("secret"),                 // если сервер запущен с JEV_API_KEY
	jevos.WithModel("jev-latest"),              // по умолчанию "jev-latest"
	jevos.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
)
```

Проверка готовности сервера: `c.Health(ctx)` (запрос `GET /health`).

## Описание ответов

| Тип вопроса | Поля `Answer` |
|-------------|---------------|
| да/нет (`noul`) | `Noul` — вероятность «да» |
| выбор (`choice`) | `Choice`, `Probabilities` (по названию варианта), `Confidence` |
| шкала (`score`) | `Score`, `Legend`, `Probabilities` (по индексу уровня), `Confidence`, метод `Level()` |

Все необработанные поля ответа доступны в `Answer.Raw`. Токены запроса — в `Response.Usage`.

## Ошибки

Ответ сервера со статусом не 2xx возвращается как `*jevos.APIError`:

```go
var apiErr *jevos.APIError
if errors.As(err, &apiErr) {
	fmt.Println(apiErr.StatusCode, apiErr.Body)
}
```

## Советы

- Правило, по которому нужно принять решение, пишите прямо в вопросе. Арифметику и сравнения делайте в коде.
- Если `Confidence` низкая, надёжнее отправить случай человеку.
- По описанию авторов jevos модель рассчитана на английский язык. Русские тексты и вопросы работают, но точность может отличаться — проверяйте на своих данных.

## Тесты

```sh
go test ./...
```

Тесты используют `httptest` и не требуют запущенного сервера.

## Лицензия

[GNU GPL v3](LICENSE) или любая более поздняя версия (`GPL-3.0-or-later`).

Сервер и модель jevos — отдельный проект со своей лицензией (код под MIT), эта библиотека только обращается к нему по HTTP.
