# Руководство по HTTP API

Публичный API обслуживается службой Gateway по базовому пути `/api/v1`. Полный машиночитаемый контракт: [`backend/api/gateway/openapi.yml`](../backend/api/gateway/openapi.yml).

Внутренние интерфейсы Core по HTTP и gRPC, а также служебные токены не являются публичным API.

## Базовый URL

Для домена `dcm.example.com`:

```text
http://dcm.example.com/api/v1
```

Health endpoints находятся вне `/api/v1`:

```text
GET /health/live
GET /health/ready
```

## Аутентификация

### Вход

```bash
curl -sS -c cookies.txt \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"<password>"}' \
  http://dcm.example.com/api/v1/auth/login
```

Response содержит `access_token`, а refresh token устанавливается как HttpOnly cookie.

### Защищённый запрос

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  http://dcm.example.com/api/v1/containers
```

### Обновление токена

```bash
curl -sS -b cookies.txt -c cookies.txt \
  -X POST \
  http://dcm.example.com/api/v1/auth/refresh
```

Токен доступа проверяется службой Gateway через SSO. Для административных маршрутов дополнительно требуется соответствующее разрешение.

## Ошибки

Ошибки в формате JSON имеют единый вид:

```json
{
  "error": "безопасное сообщение для пользователя",
  "error_code": "conflict",
  "request_id": "request-correlation-id"
}
```

- `error` безопасен для отображения пользователю.
- `error_code` предназначен для программной обработки.
- `request_id` передаётся оператору для поиска журналов сервера, но не должен отображаться как основное сообщение интерфейса.
- Необработанные ошибки базы данных, Docker и вышестоящих служб клиенту не возвращаются.

Типичные статусы: `400`, `401`, `403`, `404`, `409`, `413`, `429`, `503`, `504`.

## Pagination

Конечные точки со списками используют:

```text
?page=1&limit=20
```

`page` начинается с `1`, `limit` принимает значения от `1` до `100`. Ответ содержит массив ресурсов и `total_count`. Конкретное имя массива указано в OpenAPI.

## Асинхронные операции

Операции жизненного цикла контейнеров и проектов, создание и удаление томов, удаление образов, сборки и развёртывания Compose выполняются асинхронно. Ответ HTTP `202 Accepted` означает, что сохраняемая операция создана или запрошена.

После ответа `202` периодически запрашивайте соответствующую конечную точку получения ресурса или списка. Прекратите опрос при достижении конечного состояния. Ответ `202` не означает, что ресурс Docker уже готов.

## Создание контейнера

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "api-demo",
    "image_tag": "nginx:alpine",
    "domain_prefix": "api-demo",
    "internal_port": 80
  }' \
  http://dcm.example.com/api/v1/containers
```

Значения переменных окружения передаются в `env_vars`, а подключения томов — в `volume_mounts`.

## Сборка из архива

В многочастном запросе метаданные должны идти перед частью файла `archive`:

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  -F 'tag=api-demo:v1' \
  -F 'context=.' \
  -F 'dockerfile=Dockerfile' \
  -F 'build_args={"APP_ENV":"production"}' \
  -F 'archive=@source.tar.gz' \
  http://dcm.example.com/api/v1/images/build
```

`build_args` — объект JSON. Не передавайте через него секреты.

## Build из Git

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_url": "https://github.com/example/public-app.git",
    "ref": "main",
    "tag": "api-demo:v1",
    "context": ".",
    "dockerfile": "Dockerfile"
  }' \
  http://dcm.example.com/api/v1/images/build/git
```

Поддерживаются только публичные репозитории по HTTPS на разрешённых серверах.

## Загрузка Compose и источник Git

Загрузка архива или отдельного файла:

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  -F 'project_name=api-compose' \
  -F 'compose_file=docker-compose.yml' \
  -F 'archive=@project.tar.gz' \
  http://dcm.example.com/api/v1/projects/compose
```

Источник Git:

```bash
curl -sS \
  -H 'Authorization: Bearer <access-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "project_name": "api-compose",
    "repo_url": "https://github.com/example/public-compose.git",
    "ref": "main",
    "compose_file": "deploy/docker-compose.yml"
  }' \
  http://dcm.example.com/api/v1/projects/compose/git
```

## Logs через SSE

1. Получите ticket обычным Bearer request:

   ```bash
   curl -sS -X POST \
     -H 'Authorization: Bearer <access-token>' \
     "http://dcm.example.com/api/v1/containers/${CONTAINER_ID}/logs/stream/ticket"
   ```

2. Откройте поток с одноразовым билетом:

   ```text
   GET /api/v1/containers/{id}/logs/stream?ticket={ticket}&tail=200&follow=true
   ```

Билет действует недолго и используется только один раз. Токен Bearer на маршруте потока не поддерживается. Строка запроса также поддерживает параметры `since` и `timestamps` согласно OpenAPI.

## Терминал через WebSocket

1. Получите ticket через `POST /containers/{id}/terminal/ticket`.
2. Подключитесь к `/containers/{id}/terminal?ticket=...&cmd=...&rows=24&cols=80` по WebSocket.
3. Источник страницы должен быть разрешён службой Gateway, контейнер должен иметь состояние `running`, а команда — входить в список, разрешённый на сервере.

## Административный API

Административные конечные точки находятся под `/api/v1/admin` и охватывают пользователей, общие ресурсы, мониторинг, рабочие настройки, отчёты и аудит. Наличие роли в клиентской части не является авторизацией: Gateway проверяет разрешение каждого запроса через SSO.

`PUT /admin/config` принимает полный объект `SystemConfig`. Сначала получите текущий объект, затем измените нужные поля и отправьте весь объект обратно, чтобы не потерять поля, добавленные новой версией.

[OpenAPI](../backend/api/gateway/openapi.yml) · [Аутентификация](authentication.md) · [К оглавлению](README.md)
