# Pomodoro — помидор-таймер экосистемы

Настольное приложение под Windows 10/11 (Wails v2): единый Go-процесс с UI на React, embedded REST/MCP API и публикацией событий в Kafka. Главный источник фактов работы для бота-мотиватора: «помидоры идут — работа идёт».

## Возможности

- Стейт-машина таймера: `idle → focus → short_break / long_break` по блокам дня из `settings.day_blocks` (например `[4,4]`: 4 помидора, длинный перерыв, ещё 4).
- Пауза/резюм с накоплением `paused_total_seconds` (по модели данных v1.0).
- Автостарт перерыва после фокуса и фокуса после перерыва (настраивается).
- «Висящие» сессии (сон/крэш) при следующем запуске закрываются как `interrupted`.
- Привязка «что делал»: текстовая метка ИЛИ задача из внешнего источника (REST tasks, URL настраивается); ретро-привязка завершённых сессий за день.
- План помидоров дня: слоты раздаются задачам автоматически по их трудозатратам (`effort_minutes`, помидор ≈ длительность фокуса), задачи с `requires_pomodoro=false` и события с фиксированным временем в план не попадают; каждый слот редактируется вручную (задача + индивидуальные длительности фокуса/перерыва), правки переживают перегенерацию; «Фокус» берёт задачу из первого невыполненного слота. Помидоры не привязаны ко времени суток.
- Пресеты помидорного дня: текущий план (число слотов + длительности) сохраняется под именем и назначается на дни недели; в день с пресетом план строится из него, без пресета — из `settings.day_blocks` и дефолтных длительностей.
- Прогресс дня: горизонтальные точки по блокам, длинные перерывы визуально отделены увеличенным расстоянием.
- Оверлей: маленькое frameless always-on-top полупрозрачное окно поверх всех приложений с убывающим круговым таймером; перетаскивание мышью, размер/прозрачность/цифры настраиваются на лету; двойной клик по кругу (или Esc) — вернуть главное окно; при остановке таймера оверлей сам возвращается в главное окно.
- Звук окончания: встроенный сигнал (go:embed) или свой файл (wav/mp3/ogg), воспроизведение на фронте через HTML Audio.
- Персистентность: PostgreSQL 16 (GORM + golang-migrate), outbox → Kafka `pomodoro.events` (CloudEvents 1.0, только focus-события). Недоступность Kafka работе не мешает — события копятся в outbox.

## Структура

```
pomodoro/
├── main.go               входная точка Wails, asset handler звука
├── app.go                App: биндинги для фронта, инициализация ядра, оверлей
├── plan.go               биндинги плана дня и пресетов
├── wails.json            конфиг Wails CLI
├── docker-compose.yml    PostgreSQL 16 на порту 5434
├── build/bin/            артефакт сборки (pomodoro.exe), в git не попадает
├── frontend/             React 18 + TypeScript + Vite + Tailwind
│   └── src/
│       ├── App.tsx                   главный экран, режимы main/overlay
│       ├── api.ts                    обёртка над window.go / window.runtime
│       └── components/               TimerRing, TimerPie, DayProgress, BindingPicker,
│                                     PlanToday, SessionsToday, SettingsView, Overlay
└── internal/
    ├── config/           viper: POMO_* поверх .env
    ├── engine/           стейт-машина таймера (ядро бизнес-логики) + тесты
    ├── plan/             план помидоров дня: слоты, распределение по effort, пресеты + тесты
    ├── handler/          Gin REST /api/v1 + MCP /mcp + httptest-тесты
    ├── relay/            outbox-релей → Kafka (sarama) + тесты
    ├── tasksclient/      клиент внешнего источника задач + тесты
    ├── store/            интерфейсы репозиториев, GORM-реализация
    │   └── memstore/     in-memory реализация для тестов
    ├── models/           GORM-модели: sessions, settings, plan_slots, presets,
    │                     preset_schedule, events_outbox
    ├── migrate/          golang-migrate, встроенные SQL-миграции
    ├── sound/            встроенный сигнал ding.wav (go:embed)
    └── logger/           zap
```

## Запуск

1. PostgreSQL — в каталоге проекта (Docker в WSL):

```powershell
wsl -d Ubuntu-22.04 -- docker compose up -d
```

2. Kafka (опционально) — в каталоге `infra/` экосистемы:

```powershell
wsl -d Ubuntu-22.04 -- docker compose up -d
```

3. Сборка и запуск:

```powershell
wails build
.\build\bin\pomodoro.exe
```

Фронт собирается автоматически (`-s` — пропустить пересборку фронта). Режим разработки: `wails dev`.

## Конфигурация

`.env` в каталоге проекта или рядом с исполняемым файлом (образец — `.env.example`), переменные окружения с префиксом `POMO_` имеют приоритет.

| Переменная | По умолчанию | Смысл |
|---|---|---|
| POMO_HTTP_PORT | 8082 | порт embedded API+MCP |
| POMO_DB_HOST / POMO_DB_PORT | localhost / 5434 | PostgreSQL |
| POMO_DB_USER / POMO_DB_PASSWORD / POMO_DB_NAME | pomodoro | доступ к БД |
| POMO_DB_SSLMODE | disable | sslmode |
| POMO_KAFKA_BROKERS | localhost:9094 | брокеры через запятую |
| POMO_KAFKA_TOPIC | pomodoro.events | топик событий |
| POMO_TASKS_URL | http://localhost:8081 | внешний источник задач |
| POMO_TASKS_SOURCE | tasks | значение `source` в привязке |
| POMO_LOG_LEVEL | info | debug/info/warn/error |

Пользовательские настройки таймера (длительности, блоки дня, автостарты, звук, оверлей) живут в таблице `settings` и редактируются в UI.

## API (localhost:8082, префикс /api/v1)

- `GET /healthz` — статус.
- `GET /api/v1/state` — снапшот стейт-машины (фаза, остаток, блоки дня).
- `GET /api/v1/sessions?from&to` — сессии за период (RFC3339 или YYYY-MM-DD, по умолчанию сегодня).
- `GET /api/v1/sessions/active` — активная сессия (`session: null`, если нет).
- `POST /api/v1/sessions` — старт: `{kind: focus|break|next, label?, task?{source, external_id, title_snapshot}}`.
- `POST /api/v1/sessions/{id}/stop` — `{outcome: completed|abandoned}` (по умолчанию abandoned).
- `POST /api/v1/sessions/{id}/pause` / `POST /api/v1/sessions/{id}/resume` — пауза/резюм.
- `PATCH /api/v1/sessions/{id}` — ретро-привязка `{label}` или `{task}`.
- `GET /api/v1/settings` / `PUT /api/v1/settings` — настройки.

Ошибки: `{"error":{"code":"...","message":"..."}}` (`session_active`, `no_active_session`, `already_paused`, `not_paused`, `not_active`, `not_found`, `invalid_input`, `not_focus`).

API рассчитано на локальное использование одним пользователем: аутентификации нет, CORS открыт (`*`), чтобы MCP-клиенты и локальные инструменты работали без настройки. Осознанный компромисс: любой сайт в браузере может дёргать `localhost:8082`; не выставляйте порт наружу.

## MCP (localhost:8082/mcp, streamable HTTP)

Тулы: `get_active_session`, `start_focus`, `stop_session`, `get_today_stats`, `get_day_plan`, `set_plan_slot` (задача/метка слота + индивидуальные `focus_minutes`/`break_minutes`, -1 — вернуть дефолт), `refresh_day_plan`.

## События (Kafka `pomodoro.events`, CloudEvents 1.0)

Только для focus-сессий: `pomodoro.started`, `pomodoro.completed`, `pomodoro.abandoned`, `pomodoro.interrupted`, `pomodoro.relabeled`. `subject` = session_id, payload несёт `task {source, external_id, title_snapshot} | null`, `label`, длительности. Ключ партиции — aggregate_id.

## Тесты

```powershell
go test ./...
```

- `internal/engine` — стейт-машина: пауза/резюм с накоплением, блоки дня и длинные перерывы, автостарты, interrupted при запуске, ролловер дня, ретро-привязка, валидация настроек и day_blocks.
- `internal/plan` — распределение слотов по effort_minutes, пресеты и длительности, сохранение закреплённых слотов при недоступном tasks.
- `internal/handler` — httptest: полный REST-контракт, коды ошибок.
- `internal/relay` — конверт CloudEvents, порядок публикации, поведение при недоступной Kafka.
- `internal/tasksclient` — поиск/фильтрация задач, обработка ошибок источника.

## Лицензия

MIT, см. [LICENSE](LICENSE).
