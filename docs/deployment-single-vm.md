# Развёртывание Docker Cloud Manager на одной машине

Инструкция описывает установку полного набора служб DCM через корневой `docker-compose.yml`. Все приложения, базы данных, очередь, объектное хранилище, реестр образов и Traefik запускаются на одной машине с Linux.

> [!WARNING]
> Текущий Compose принимает внешний трафик по HTTP на порту `80`. HTTPS/ACME не настроены.

## Ограничения текущего варианта

- Один Docker Engine является единственной средой выполнения контейнеров.
- Внешняя точка входа — HTTP `:80`.
- Registry публикуется только на `127.0.0.1:5000` и рассчитан на тот же сервер Docker.
- PostgreSQL, ClickHouse, RabbitMQ и MinIO запускаются на той же машине.
- Высокая доступность и автоматическое восстановление узла не предусмотрены.

## 1. Требования

Подготовьте:

- Linux-машину с публичным IPv4 или IPv6;
- Docker Engine и Docker Compose plugin;
- пользователя с доступом к Docker daemon;
- свободный входящий TCP-порт `80`;
- домен панели, например `dcm.example.com`;
- отдельный wildcard-домен приложений, например `*.apps.example.com`;
- `git`, `curl`, `openssl`, Python 3 и утилиту `dig` либо аналог для проверки DNS.

Проверьте окружение:

```bash
docker version
docker compose version
docker info
sudo ss -ltnp '( sport = :80 )'
```

Последняя команда не должна показывать процесс, занимающий `0.0.0.0:80` или `[::]:80`.

## 2. Настройте DNS

Создайте записи, ведущие на адрес сервера:

```text
dcm.example.com       A/AAAA  <server-ip>
*.apps.example.com    A/AAAA  <server-ip>
```

Проверьте обе записи:

```bash
dig +short dcm.example.com
dig +short test.apps.example.com
```

Оба имени должны возвращать адрес сервера. `DCM_HOST` будет равен `dcm.example.com`, а `BASE_DOMAIN` — `apps.example.com`.

## 3. Расширьте пул Docker-сетей

DCM создаёт отдельную bridge-сеть для каждого пользователя. Стандартного пула Docker может не хватить уже после нескольких десятков сетей.

Перед первым запуском добавьте свободный адресный диапазон в `/etc/docker/daemon.json`. Не используйте диапазон, пересекающийся с LAN, VPN, cloud VPC или существующими Docker networks.

```json
{
  "default-address-pools": [
    {
      "base": "10.10.0.0/16",
      "size": 24
    }
  ]
}
```

Если файл уже содержит настройки, добавьте ключ в существующий JSON-объект. Затем проверьте JSON и перезапустите Docker:

```bash
python3 -m json.tool /etc/docker/daemon.json >/dev/null
sudo systemctl restart docker
docker info
```

Перезапуск Docker останавливает работающие контейнеры; на используемом сервере предварительно согласуйте окно обслуживания.

## 4. Получите исходный код

```bash
git clone https://github.com/callmerussell04/docker-cloud-manager.git
cd docker-cloud-manager
```

Все следующие Compose-команды выполняются из корня репозитория.

## 5. Создайте env-файл

Скопируйте публичный шаблон:

```bash
cp backend/.env.example backend/.env
chmod 600 backend/.env
```

Сгенерируйте независимые значения для секретов:

```bash
openssl rand -hex 32
```

Выполните команду отдельно для каждого секрета и замените значения `change_me_*`. Как минимум задайте уникальные значения для:

- `POSTGRES_PASSWORD`;
- `JWT_SECRET`;
- `INTERNAL_SERVICE_TOKEN`;
- `SSO_ADMIN_PASSWORD`;
- `RABBITMQ_PASSWORD`;
- `MINIO_ROOT_PASSWORD` и совпадающего с ним `OBJECT_STORAGE_SECRET_KEY`;
- `CLICKHOUSE_PASSWORD`.

Переменные Keycloak обязательны только при запуске дополнительного Keycloak Compose-файла, но оставлять опубликованные значения `change_me_*` небезопасно.

## 6. Настройте домены и браузерный доступ

Для примера выше установите:

```env
SSO_ADMIN_EMAIL=admin@example.com
DCM_HOST=dcm.example.com
BASE_DOMAIN=apps.example.com
FRONTEND_API_URL=http://dcm.example.com/api/v1
CORS_ALLOWED_ORIGINS=http://dcm.example.com
GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS=http://dcm.example.com
COOKIE_SECURE=false
COOKIE_SAMESITE=lax
```

`FRONTEND_API_URL` и `BASE_DOMAIN` встраиваются в клиентскую часть во время сборки. После изменения этих значений её необходимо пересобрать.

Для реестра образов на одной машине оставьте:

```env
REGISTRY_API_URL=https://registry-backend:5000
REGISTRY_URL=registry:5000
REGISTRY_PUBLIC_URL=registry:5000
```

OIDC по умолчанию выключен:

```env
SSO_OIDC_ENABLED=false
```

Полное назначение переменных описано в [справочнике конфигурации](configuration.md).

Перед первым запуском выполните раздел «TLS и изоляция Registry» ниже.

## 7. Проверьте конфигурацию

```bash
docker compose --env-file backend/.env config --quiet
```

Успешная команда ничего не выводит и завершается с кодом `0`. Ошибка вида `VARIABLE is required` означает, что обязательное значение отсутствует.

## 8. Запустите стек

```bash
docker compose --env-file backend/.env up -d --build
```

Compose последовательно запускает базы данных и инфраструктурные службы, применяет миграции PostgreSQL и ClickHouse, а затем запускает службы DCM. Первая сборка может занять несколько минут.

Проверьте состояние:

```bash
docker compose --env-file backend/.env ps --all
```

Ожидаемый результат:

- `core_db`, `sso_db`, `clickhouse`, `rabbitmq`, `minio` и `frontend` имеют состояние `healthy`;
- `gateway`, `sso`, `core`, `builder`, `telemetry`, `traefik` и `registry` находятся в состоянии `running`;
- `migrate_core`, `migrate_sso`, `migrate_reports` завершились с кодом `0`.

При ошибке перейдите к разделу [«Службы не запускаются или проверка готовности возвращает 503»](troubleshooting.md#службы-не-запускаются-или-проверка-готовности-возвращает-503).

## 9. Проверьте доступность

На сервере проверьте маршрутизацию с правильным HTTP-заголовком `Host`:

```bash
curl -fsS -H 'Host: dcm.example.com' http://127.0.0.1/health/live
curl -fsS -H 'Host: dcm.example.com' http://127.0.0.1/health/ready
```

С внешней машины:

```bash
curl -fsS http://dcm.example.com/health/live
curl -fsS http://dcm.example.com/health/ready
curl -I http://dcm.example.com/
```

Проверка жизнеспособности должна вернуть `status: ok`. Проверка готовности возвращает HTTP `200`, только когда Gateway видит Core по gRPC и HTTP, SSO по gRPC и Telemetry по HTTP.

## 10. Выполните первый вход

Откройте:

```text
http://dcm.example.com/
```

Используйте значения из `backend/.env`:

- имя пользователя — `SSO_ADMIN_USERNAME`;
- пароль — `SSO_ADMIN_PASSWORD`.

После входа:

1. Откройте системный мониторинг и убедитесь, что ресурсы сервера доступны.
2. Создайте тестового пользователя и назначьте квоты.
3. Выполните [минимальную проверку основных возможностей](user-guide.md#минимальная-проверка-основных-возможностей) из руководства пользователя.

## Остановка

Безопасная остановка с сохранением данных:

```bash
docker compose --env-file backend/.env down
```

> [!CAUTION]
> Не добавляйте `-v`, если не требуется безвозвратно удалить базы данных, object storage, registry и очереди.

## Следующие шаги

- [Руководство администратора](admin-guide.md)
- [Аутентификация и OIDC](authentication.md)
- [Диагностика](troubleshooting.md)

[К оглавлению документации](README.md)

## TLS и изоляция Registry

Перед запуском подготовьте `REGISTRY_CERTS_DIR` (например `/etc/dcm/registry`) с правами, ограничивающими чтение private keys:

- `ca.crt` — CA для TLS;
- `registry.crt`, `registry.key` — TLS certificate/key для Core edge, SAN включает `registry`;
- `backend.crt`, `backend.key` — TLS certificate/key backend, SAN включает `registry-backend`;
- `signing.crt`, `signing.key` — отдельный RSA signing certificate/key Core. Используйте отдельный self-signed certificate либо certificate, непосредственно доверенный в `signing.crt` Registry. Приватный signing key монтируется только в Core.

Создайте сертификаты своей CA, задайте `REGISTRY_HTTP_SECRET` независимо от остальных секретов. Включите `br_netfilter` и `net.bridge.bridge-nf-call-iptables=1`, `net.bridge.bridge-nf-call-ip6tables=1`. Compose требует iptables backend Docker и устанавливает ограничения для отдельных bridges `br-dcm-build`/`br-dcm-egress`; не используйте эти имена для других сетей. Диапазон `172.30.252.0/24` должен быть свободен.

Для single-server конфигурации установите `REGISTRY_URL=registry:5000` и `REGISTRY_PUBLIC_URL=registry:5000`. Добавьте на Docker host `registry` в `/etc/hosts` с адресом `127.0.0.1` и установите `ca.crt` в `/etc/docker/certs.d/registry:5000/ca.crt`. Docker должен доверять этой CA. Канонический TLS realm должен быть достижим по одному имени и из Kaniko, и из daemon. Для другого DNS имени обновите SAN, host DNS, оба адреса и network alias согласованно.

Порт `127.0.0.1:5000` принадлежит TCP forwarder `registry-edge`, который передаёт TLS только в аутентифицированный listener Core. Core не подключается к сети сборки. Сам Registry не публикует порты и находится в отдельной внутренней сети. Не добавляйте его в `build_net`, `dcm_net` или Traefik. Запрос без credentials к `https://registry:5000/v2/` должен вернуть `401` и Bearer challenge; plaintext HTTP и Core management paths на этом порту недоступны.

`build-network-policy` требует `CAP_NET_ADMIN` и host network для установки host firewall. `build-egress` обеспечивает доступ Dockerfile к внешним пакетам по HTTP(S) через `HTTP_PROXY`/`HTTPS_PROXY`; клиенты, игнорирующие proxy, не получают прямой интернет-доступ. Builder ждёт готовности policy. Для обновления существующего стенда остановите новые сборки, дождитесь завершения текущих, затем пересоздайте `build_net` как internal network. Согласуйте действующий host firewall с этими правилами; не снимайте ограничения при остановке worker.

Изолированная проверка (только disposable test database):

```bash
cd backend
CORE_TEST_REGISTRY_ISOLATION=1 CORE_TEST_DATABASE_URL='postgres://.../dcm_core_test?sslmode=disable' \
  GOCACHE=/tmp/go-build-cache go test -count=1 -timeout=5m ./tests/core/integration/registry_isolation
```

Тест создаёт собственные Registry storage, bridge и execution containers, проверяет реальные Dockerfile RUN с прочитанным job credential и удаляет свои fixtures. Он не устанавливает host firewall и не проверяет policy container; перед эксплуатацией отдельно проверьте сетевую политику из execution network.

Для изолированной проверки host firewall с уникальными временными bridges/chains и их удалением после теста:

```bash
cd backend
CORE_TEST_NETWORK_POLICY=1 GOCACHE=/tmp/go-build-cache \
  go test -count=1 -timeout=3m -run TestBuildNetworkPolicy ./tests/core/integration/registry_isolation
```

## Go toolchain and build inputs

Reviewed 2026-10-17: [Go release policy and patch notes](https://go.dev/doc/devel/release)
and [official downloads](https://go.dev/dl/?mode=json) list Go 1.27.2 and 1.26.9 as
supported patched releases. DCM selects **1.26.9**, retaining the host's existing
1.26 release line while replacing the unsupported module minimum 1.25.7 and the
host baseline 1.26.1. No new language features or third-party dependency upgrades
are part of this change.

`backend/go.mod` sets the minimum to 1.26.9. Canonical Make targets export exactly
`GOTOOLCHAIN=go1.26.9`; the `go` directive alone is a minimum and does not prevent a
newer installed compiler from being selected. For direct commands, export
`GOTOOLCHAIN=go1.26.9` explicitly. Keep the module version and all five Dockerfile
builder pins synchronized when updating. Toolchain downloads use Go's checksum
database; do not disable checksum verification.

All five service Dockerfiles use these verified multi-platform image indexes:

| Input | Pinned reference |
| --- | --- |
| Compiler | `golang:1.26.9-alpine3.24@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade` |
| Runtime | `alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6` |

Docker sets `GOTOOLCHAIN=local`, so module changes cannot silently download another
compiler. Module downloads are checked against committed `go.sum` and verified
with `go mod verify`. Local and Docker service builds use `CGO_ENABLED=0`,
`-mod=readonly -trimpath -buildvcs=false`. Core and Gateway additionally install
`ca-certificates=20260909-r0` and `git=2.54.0-r0`; their complete runtime package
closure, including transitive dependencies and base packages, is pinned in
`backend/dockerfiles/runtime-packages.lock`. APK verifies signed packages.
Unavailable locked versions fail the build; update the lock deliberately alongside
base image pins. This is still an online build, not an offline repository archive
or an OS-image CVE audit. The package lock was resolved and verified for Linux
amd64; validate package availability for other target architectures before use.

From `backend/`, verify with:

```bash
make build
make test-fast
make vet
make test-race
make test
make vulncheck
```

`make vulncheck` installs **govulncheck v1.9.0** in a versioned ignored tools
folder, scans `./cmd/...` (production roots, excluding test-only roots), and scans
`bin/builder`, `bin/core`, `bin/gateway`, `bin/sso`, and `bin/telemetry` separately.
It runs all scans even if one reports findings. The vulnerability database is
live; record the scan date, compiler, scanner version, GOOS/GOARCH, and build
settings when comparing results. Binary symbol inclusion is conservative evidence,
not proof that an untrusted request reaches a vulnerable operation. Third-party
advisories require separate dependency and transport-specific assessment.

### Standard-library applicability

The baseline assessment used local Go 1.26.1, while Docker used floating
`golang:alpine3.22` and `alpine:latest`. A local scan therefore could not establish
which Go standard library a Docker build contained.

- [GO-2026-4869](https://pkg.go.dev/vuln/GO-2026-4869): untrusted tar uploads reach
  Builder's `archive/tar.Reader.Next` through its extractor and storage adapter.
  The fix landed in Go 1.25.9 / 1.26.2 and is included in 1.26.9. This is a
  relevant parser boundary, without claiming a demonstrated DCM exploit.
- [GO-2026-6608](https://pkg.go.dev/vuln/GO-2026-6608): Core accepts build and
  Compose multipart uploads through `Request.MultipartReader` and `Reader.NextPart`,
  which parses MIME headers. The advisory describes an arbitrarily long line
  allocation when the remaining parser memory limit is below 400 bytes; fixed
  in Go 1.26.9 / 1.27.2. Upload limits alone do not establish non-applicability.
  This patch includes the fix; a reachable parser is not proof of a successful
  attack against every HTTP service.
- The selected patch also includes intervening TLS, X.509, HTTP, URL, MIME and
  textproto fixes described in the release history. Platform-specific and unused
  protocol reports must be assessed against Linux service builds and actual
  callers, without blanket exclusions.

### Verification snapshot (2026-10-17)

Environment: Linux/amd64, Go 1.26.9, `CGO_ENABLED=0`, govulncheck v1.9.0,
`https://vuln.go.dev` database last modified `2026-10-08T22:31:09Z`.
Source scan: `CGO_ENABLED=0 govulncheck ./cmd/...`; binary scans:
`govulncheck -mode=binary bin/<service>` after `make build`.
Compiler and settings were confirmed with `go version -m`.

| Scan | Symbol-level advisory IDs | Standard-library findings |
| --- | ---: | ---: |
| Production roots `./cmd/...` | 20 | 0 |
| Builder binary | 19 | 0 |
| Core binary | 19 | 0 |
| Gateway binary | 8 | 0 |
| SSO binary | 7 | 0 |
| Telemetry binary | 10 | 0 |

Counts are unique IDs per scan, not additive, and are not counts of demonstrated
exploits. Remaining symbol findings concern Docker, go-archive, quic-go,
amqp091-go, x/net and gRPC. Some x/net advisories share IDs with patched standard
library copies: updating Go fixes `stdlib`, not the separately versioned x/net
module. `make vulncheck` intentionally remains nonzero while these dependencies
are affected; there are no suppressions.
For example, GO-2026-6253 is an unreviewed archive-extraction advisory while DCM's
direct go-archive use is `TarWithOptions`; scanner inclusion alone does not
establish a DCM host escape. Review protocol and platform preconditions before
classifying the other third-party reports or accepting exclusions.

Compatibility checks passed: all five local service binaries, canonical fast
checks (699 tests, 84 skipped), race checks (699 tests, 84 skipped), vet, generated
protobuf drift checks, and the full integration suite (711 tests, 2 skipped).
The integration run included Docker, PostgreSQL, ClickHouse, RabbitMQ and MinIO;
the two skipped suites require explicit Registry isolation/network-policy opt-ins.
Module tidy changed only the existing go-connections dependency's direct/indirect
classification; dependency versions and `go.sum` were unchanged.

Repeating `make build` produced identical SHA-256 checksums for all five binaries.
Installing the runtime lock in the pinned Alpine image reproduced exactly its
29 package versions, with no extra or missing packages.
