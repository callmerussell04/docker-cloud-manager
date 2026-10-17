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
- Docker Engine 29.5.1 или новее с актуальными исправлениями безопасности и Docker Compose plugin;
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

Обновление Go SDK в DCM не обновляет Docker daemon на сервере. Версия Engine 29.5.1 исправляет [уязвимости копирования архивов в контейнер](https://github.com/moby/moby/security/advisories/GHSA-x86f-5xw2-fm2r), включая связанные гонки при обработке путей. Builder передаёт несжатый tar в ещё не запущенный контейнер Kaniko; исходные ZIP/TAR.GZ не отправляются daemon напрямую. Сохраняйте этот порядок и обновляйте Engine отдельно от DCM.

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

### Network dependencies and compatibility

The network remediation uses bounded, explicitly selected upgrades:

| Module | Previous | Selected | Reason |
| --- | --- | --- | --- |
| `google.golang.org/grpc` | 1.79.3 | 1.83.2 | HTTP/2 frame buffering/flood protections and missing-authority rejection |
| `golang.org/x/net` | 0.51.0 | 0.60.0 | HTTP/2 trailer, window, flow-control and HPACK fixes; earlier IDNA fixes |
| `github.com/rabbitmq/amqp091-go` | 1.11.0 | 1.15.0 | Frame/field bounds, SASL credential handling and reconnect corrections |
| `github.com/quic-go/quic-go` | 0.59.0 | 0.59.1 | HTTP/3 trailer validation security backport |

The gRPC and x/net module requirements also raise x/crypto, x/sys, x/text,
x/sync, OpenTelemetry API/metric/trace and genproto RPC versions. These are
required graph changes, not a general dependency refresh. Protobuf stays at
1.36.11, including the pinned code generator; generated RPC definitions do not
change. The selected modules support the pinned Go 1.26.9 toolchain.

Review these upstream notes when updating again:
[gRPC releases](https://github.com/grpc/grpc-go/releases),
[AMQP 1.13](https://github.com/rabbitmq/amqp091-go/releases/tag/v1.13.0),
[1.14](https://github.com/rabbitmq/amqp091-go/releases/tag/v1.14.0),
[1.15](https://github.com/rabbitmq/amqp091-go/releases/tag/v1.15.0), and
[QUIC 0.59.1](https://github.com/quic-go/quic-go/releases/tag/v0.59.1).

- DCM uses gRPC's native HTTP/2 client/server transport. Internal authentication
  does not eliminate pre-authentication frame-parser exposure. The upgrade covers
  [GO-2026-6061](https://pkg.go.dev/vuln/GO-2026-6061),
  [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348), and
  [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443). DCM does not configure xDS
  or its RBAC engine; those portions of the reports do not describe its typed
  scope/token authorization. Strict RPC paths remain enabled. Default write/read
  buffer pooling changes are exercised by client/server and race checks. TLS
  authority validation now uses the leaf certificate; configure service names
  against that certificate. Unexpected upstream gRPC errors still pass through
  DCM's safe transport error mapping.
- Go's standard library and x/net are patched separately. The selected x/net
  fixes GO-2026-4918, GO-2026-5026, GO-2026-6603, GO-2026-6610,
  GO-2026-6611, GO-2026-6612 and GO-2026-6617. Assess HTTP/2 server and client
  exposure separately; an HTTP/1 deployment is not an exclusion for gRPC.
- AMQP parsing fixes apply to a malicious or compromised broker, not a direct
  unauthenticated HTTP request. DCM retains persistent JSON publications,
  confirms, durable quorum queues/DLX, manual ACK/NACK and `prefetch=1`.
  New short/long string bounds reject invalid encodings; current queue names,
  routing keys and message fields remain compatible. URI-derived AMQPS now has
  an explicit TLS 1.2 minimum; development Compose still uses AMQP. Version 1.15
  preserves caller SASL credentials while redacting the connection's copy.
  `amqp.Dial` leaves `Config.Recovery` nil: automatic recovery is disabled and
  DCM continues to own reconnect/topology handling. Application attempt budgets
  and publisher-confirm cancellation hardening remain separate work.
- Gin imports HTTP/3 support, but DCM starts ordinary HTTP servers, proxies
  HTTP/SSE/WebSocket and uses native gRPC; it does not start a QUIC listener or
  use an HTTP/3 client transport. GO-2026-5676 is patched anyway by the small
  quic-go backport. Gorilla WebSocket 1.5.3 already contains the reported masking
  fix. No new protocol or authentication mode is enabled.

### Verification snapshot (2026-10-17)

Environment: Linux/amd64, Go 1.26.9, `CGO_ENABLED=0`, govulncheck v1.9.0,
`https://vuln.go.dev` database last modified `2026-10-08T22:31:09Z`.
Source scan: `CGO_ENABLED=0 govulncheck ./cmd/...`; binary scans:
`govulncheck -mode=binary bin/<service>` after `make build`.
Compiler and settings are inspectable with `go version -m`.

| Scan | Before: symbol-level IDs | After: symbol-level IDs | After: standard-library findings |
| --- | ---: | ---: | ---: |
| Production roots `./cmd/...` | 20 | 3 | 0 |
| Builder binary | 19 | 3 | 0 |
| Core binary | 19 | 2 | 0 |
| Gateway binary | 8 | 0 | 0 |
| SSO binary | 7 | 0 | 0 |
| Telemetry binary | 10 | 2 | 0 |

Counts are unique IDs per scan, not additive, and do not count demonstrated
exploits. Gateway and SSO have no symbol-level findings; their module-only
reports still require applicability review.

Network dependency updates remove all 17 network-related symbol-level findings
from the preceding 20-ID production-root baseline. Remaining Docker/archive
findings are reviewed separately.
`make vulncheck` remains nonzero and reports them without suppressions.

Residual applicability is deliberately narrower than a blanket module exclusion:

- GO-2026-4883 concerns Docker daemon plugin-install privilege validation;
  GO-2026-4887 concerns daemon AuthZ plugins and oversized bodies. DCM links the
  Docker client/types, not the daemon plugin authorization handlers, and does
  not expose plugin installation. Module-wide unreviewed reports flag client
  symbols too. This excludes those server operations from DCM binaries, not the
  independently installed host daemon from assessment. See the upstream
  [plugin privilege advisory](https://github.com/moby/moby/security/advisories/GHSA-pxq6-2prw-chj9)
  and [AuthZ advisory](https://github.com/moby/moby/security/advisories/GHSA-x744-4wpc-v9h2).
- GO-2026-6253 concerns archive extraction. Builder directly calls
  `go-archive.TarWithOptions` to create a workspace tar; uploads use DCM's
  separate extractor, not `go-archive.Untar`. Builder also calls the Docker
  daemon's archive-copy API, so host-daemon/archive risk still needs #16 review.
  Symbol inclusion alone does not establish a host escape. See the
  [extraction advisory](https://github.com/moby/go-archive/security/advisories/GHSA-hfg8-hc9c-6c3h).
- Module/package-only findings are not silently waived: Docker copy advisories
  GO-2026-5617/5668/5746 and compress/s2 GO-2026-5841 remain part of #16's
  archive/daemon assessment. GO-2026-5932 concerns x/crypto/openpgp, which is not
  imported by DCM's production graph; local password authentication uses bcrypt.
  These exclusions apply to the inspected Linux/amd64 service builds only;
  other platforms or new protocol/copy/authentication callers require reassessment.

Compatibility checks passed: all five service builds, focused HTTP/gRPC/OIDC and
Telemetry tests (215 tests, 1 Docker opt-in skipped), canonical fast and race
suites (700 tests each, 85 integration skips each), vet, protobuf drift checks,
module verification/tidy consistency, and lint for new changes (zero issues).
RabbitMQ smoke tests passed with race detection, including confirms, persistence,
permanent/malformed dead-letter delivery, transient redelivery/ACK and cancellation.
The full suite passed (712 tests, 2 Registry isolation/network-policy opt-ins
skipped), including PostgreSQL, ClickHouse, RabbitMQ, MinIO and Docker flows.

The preceding toolchain verification reproduced identical SHA-256 checksums for
all five binaries on repeated builds and exactly the runtime lock's 29 package
versions in the pinned Alpine image. Network remediation changes module inputs;
it does not change those compiler, image or APK pins.
