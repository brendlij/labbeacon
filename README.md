# homelab-agent

Ein kleines Go-Binary pro Server: Systemmetriken, Docker-Inventar und Service-Checks
per MQTT an Home Assistant. Jeder Agent erscheint durch MQTT Discovery als eigenes
Gerät. Linux ist die primäre Zielplattform; das Binary baut auch unter Windows.

**Voraussetzungen:** erreichbarer MQTT-Broker, aktivierte MQTT-Integration in Home
Assistant und Go 1.25+ oder Docker Engine mit Compose auf dem Linux-Host. Der Agent
installiert weder einen Broker noch Home Assistant. Noch kein öffentliches Image?
Das Compose-Beispiel baut direkt aus dem geklonten Repository.

> Screenshot-Platzhalter: Home-Assistant-Gerät „Server 1“ mit gruppierten Sensoren.
> Screenshot-Platzhalter: Dashboard mit CPU, RAM, Netzwerk und Service-Verfügbarkeit.

## Schnellstart mit Docker (Linux)

Im geklonten Repository:

```sh
cp configs/config.docker.example.yaml configs/config.yaml
cp .env.example .env
# configs/config.yaml: eindeutige agent.id, agent.name und mqtt.broker eintragen.
# .env: MQTT_USER und MQTT_PASSWORD eintragen (oder leer lassen).
docker compose run --rm homelab-agent -config /etc/homelab-agent/config.yaml -check-config
docker compose up -d --build
docker compose logs -f
```

Das Gerät erscheint unter **Einstellungen → Geräte & Dienste → MQTT**. Die erste
Netzwerk-Durchsatzmessung kommt im zweiten Messzyklus. Für weitere Server dieselben
Schritte mit einer anderen `agent.id` wiederholen. IDs müssen brokerweit eindeutig
und dauerhaft sein; doppelte IDs führen zu MQTT-Verbindungsabbrüchen und vermischten
Geräten.

Compose bindet den Host unter `/hostfs` ein, setzt die gopsutil-Variablen `HOST_PROC`,
`HOST_SYS`, `HOST_ETC`, `HOST_VAR`, `HOST_RUN` und teilt Netzwerk-, PID- und
UTS-Namensraum. Damit beziehen sich die Werte auf den Linux-Host. Zusätzliche
Datenträger als `/hostfs/mnt/data` in `disk_paths` ergänzen. Der Bind-Mount verwendet
`rslave`, damit Host-Mounts bei passender Mount-Propagation sichtbar werden.
Docker Desktop zeigt die Linux-VM, nicht das Windows-/macOS-Hostsystem.

Der Container läuft als UID/GID 65532 ohne Linux-Capabilities. Die Config muss für
diese UID lesbar sein. Die eingebundenen Host-Verzeichnisse gewähren Lesezugriff auf
Hostdaten; das ist ein Monitoring-Container für vertrauenswürdige Hosts.

### Docker-Inventar aktivieren

1. `modules.docker.enabled: true` setzen.
2. Unter `volumes` in `compose.yaml` ergänzen:
   `/var/run/docker.sock:/var/run/docker.sock:ro` (als YAML-Listeneintrag).
3. `group_add: ["${DOCKER_GID}"]` aktivieren und starten:

```sh
export DOCKER_GID=$(stat -c %g /var/run/docker.sock)
docker compose up -d --build
```

Bei Rootless Docker den Socketpfad und die Gruppenrechte anpassen. Ein Socket-Mount
mit `:ro` verhindert **keine schreibenden Docker-API-Aufrufe**. Der Agent nutzt nur
GET-Requests, aber Socket-Zugriff ist eine weitreichende Hostberechtigung.

## Schnellstart als Binary

```sh
go build -trimpath -ldflags="-s -w -X homelab-agent/internal/version.Version=0.1.0" -o bin/homelab-agent ./cmd/homelab-agent
cp configs/config.example.yaml config.yaml
export MQTT_USER=''
export MQTT_PASSWORD=''
# Config bearbeiten: Broker, ID, Pfade und die Beispiel-Service-Checks anpassen.
./bin/homelab-agent -config config.yaml -check-config
./bin/homelab-agent -config config.yaml
```

`-version` zeigt die Build-Version. `SIGINT`/`SIGTERM` beendet laufende Checks,
publiziert bestmöglich `offline` und trennt MQTT. Unter Windows funktionieren
System- und HTTP/TCP-Checks; Diskpfade z. B. auf `C:\` setzen. Load Average kann dort
nicht verfügbar sein. Der Docker-Collector unterstützt Unix-Sockets, keine
Windows-Named-Pipes. Linux ist für Container-Deployment und systemd vorgesehen.

### systemd

Eine Unit liegt in `deploy/homelab-agent.service`. Beispielinstallation nach dem Build:

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin homelab-agent
sudo install -m 0755 bin/homelab-agent /usr/local/bin/homelab-agent
sudo install -d -m 0750 -o root -g homelab-agent /etc/homelab-agent
sudo install -m 0640 -o root -g homelab-agent config.yaml /etc/homelab-agent/config.yaml
sudo install -m 0644 deploy/homelab-agent.service /etc/systemd/system/
```

In `/etc/homelab-agent/environment` `MQTT_USER=...` und `MQTT_PASSWORD=...` ablegen
(root-eigene Datei, Modus 0600), dann `sudo systemctl daemon-reload` und
`sudo systemctl enable --now homelab-agent` ausführen. Für das Docker-Modul benötigt
der Dienstbenutzer Zugriff auf die Docker-Socket-Gruppe. Für VPN-CLI-Abfragen müssen
die jeweiligen lokalen Daemon-Sockets erreichbar sein.

## Konfiguration

Eine YAML-Datei ist die Quelle der Konfiguration; Änderungen erfordern einen
Neustart. Unbekannte Felder, mehrere YAML-Dokumente, ungültige IDs, unzulässige
Check-Typen und fehlende Pflichtwerte werden beim Start abgelehnt. Alle Module sind
im Code standardmäßig deaktiviert; die Beispieldateien aktivieren passende Module.

| Schlüssel | Standard | Bedeutung |
| --- | --- | --- |
| `agent.id` | erforderlich | Buchstaben, Ziffern, `_`, `-`; dauerhaft eindeutige ID |
| `agent.name` | erforderlich | Anzeigename des Geräts |
| `agent.poll_interval` | `20s` | Messintervall, mindestens `1s` |
| `agent.expire_after` | `60s` | Ganze Sekunden, größer als Messintervall |
| `agent.log_level` | `info` | `debug`, `info`, `warn`, `error`; JSON-Logs auf stderr |
| `mqtt.broker` | erforderlich | URL, z. B. `tcp://host:1883` oder `ssl://host:8883` |
| `mqtt.username` / `mqtt.password` | leer | Broker-Zugangsdaten |
| `mqtt.discovery_prefix` | `homeassistant` | Discovery-Präfix, passend zu Home Assistant |
| `modules.system.enabled` | `false` | Systemmetriken erfassen |
| `modules.system.disk_paths` | `["/"]` | Beliebig viele Diskpfade im Agent-Namensraum |
| `modules.docker.enabled` | `false` | Docker Engine abfragen |
| `modules.docker.socket_path` | `/var/run/docker.sock` | Unix-Socket |
| `modules.docker.timeout` | `5s` | Zeitbudget für den gesamten Inventardurchlauf |
| `modules.services.enabled` | `false` | Service-Checks aktivieren |
| `modules.services.checks` | `[]` | Liste, Felder siehe unten |
| `modules.tailscale.enabled` | `false` | Tailscale-Status abfragen |
| `modules.tailscale.command` | `tailscale` | CLI-Name oder absoluter Binarypfad, keine Shellargumente |
| `modules.tailscale.timeout` | `5s` | CLI-Zeitlimit |
| `modules.netbird.enabled` | `false` | NetBird-Status abfragen |
| `modules.netbird.command` | `netbird` | CLI-Name oder absoluter Binarypfad |
| `modules.netbird.timeout` | `5s` | CLI-Zeitlimit |

Service-Felder:

| Feld | Standard | Bedeutung |
| --- | --- | --- |
| `name` | erforderlich | Eindeutiger Name; Teil der stabilen Sensor-ID |
| `type` | erforderlich | `http` oder `tcp` |
| `url` | für HTTP erforderlich | Absolute HTTP(S)-URL |
| `expected_status` | `200` | Erwarteter HTTP-Code, 100–599 |
| `host` | für TCP erforderlich | Hostname oder IP, IPv6 ohne eckige Klammern |
| `port` | für TCP erforderlich | 1–65535 |
| `timeout` | `5s` | Positives Zeitlimit je Check |

HTTP-Checks verwenden GET, prüfen genau den Statuscode und folgen keinen Redirects.
TLS-Zertifikate werden geprüft. TCP-Checks prüfen den Verbindungsaufbau, nicht das
Anwendungsprotokoll. Höchstens acht Services werden gleichzeitig geprüft. ICMP/Ping
ist in 0.1.0 nicht implementiert; dadurch werden keine Raw-Socket-Rechte benötigt.
Module laufen parallel; ein Messdurchlauf hat maximal `poll_interval` Zeit. Bei
vielen langsamen Checks Intervall und `expire_after` erhöhen. Nicht erhobene Werte
werden nicht durch erfundene Nullwerte ersetzt, sondern laufen in HA ab.

### ENV und Secrets

`${VARIABLE}` und `$VARIABLE` werden in YAML-Stringwerten expandiert, **nach** dem
YAML-Parsing. Auch Passwörter mit Doppelpunkten oder Zeilenumbrüchen können daher
keine YAML-Struktur einschleusen. Nicht gesetzte referenzierte Variablen sind ein
Startfehler; explizit leere Variablen sind erlaubt. Bei einem Literal-Dollarzeichen
den kompletten Wert über einen direkten ENV-Override übergeben.

Direkte Overrides nach dem Einlesen:

| ENV | Config |
| --- | --- |
| `AGENT_ID`, `AGENT_NAME` | `agent.id`, `agent.name` |
| `POLL_INTERVAL`, `EXPIRE_AFTER` | entsprechende Agent-Durations |
| `LOG_LEVEL` | `agent.log_level` |
| `MQTT_BROKER`, `MQTT_USER`, `MQTT_PASSWORD` | entsprechende MQTT-Felder |

`.env` wird von **Compose** gelesen. Beim direkten Binary-Aufruf Variablen im
Prozess-Environment setzen; das Binary lädt keine `.env`-Datei. Echte Configs und
`.env` sind in `.gitignore` und `.dockerignore` ausgeschlossen.

TLS-Schemes sind `ssl`, `tls` und `wss`; zusätzlich werden `tcp` und `ws` unterstützt.
TLS verwendet mindestens Version 1.2 und die System-CA-Zertifikate. Eigene CAs über
den System-Truststore bzw. `SSL_CERT_FILE` im Linux-Container bereitstellen. mTLS
und eine Option zum Abschalten der Zertifikatsprüfung sind nicht implementiert.

## Sensoren und Ausfallverhalten

| Modul | Sensoren / Attribute |
| --- | --- |
| System | CPU %, logische Kerne, CPU-Modell; Load 1/5/15; RAM/Swap used, total, %; Disk used, total, % je Pfad; RX/TX in B/s je Interface; Uptime in s, Hostname, OS, Platform, Kernel |
| Docker | `containers_running`, `containers_total`; Running-Sensor mit `running`, `total`, `summary` und vollständigem `containers`-Array |
| Services | Connectivity-binary_sensor je Name; `response_time_ms`, `checked_at` (UTC), `check_type`, HTTP `status_code` bzw. Fehlermeldung |
| Tailscale | Eigener Online-Status, IP, Anzahl online gemeldeter Peers; IP-Liste und Backendstatus als Attribute; optional gewählter Exit-Node samt Status |
| NetBird | Management-/Signal-Verbindung, eigene IP (ggf. CIDR), verbundene Peers; Gesamtzahl als Attribut |

Docker-Details enthalten `id`, `name`, `status`, `image`, `started_at`,
`uptime_seconds` und ggf. `health`. Uptime ist bei beendeten/restartenden Containern
0; laufende und pausierte Container verwenden die Zeit seit `StartedAt`. Es werden
alle Container abgefragt, auch gestoppte. Ein Fehler beim Listen oder Inspizieren
verwirft diesen Docker-Durchlauf, damit kein unvollständiges Inventar als vollständig
erscheint. Bei sehr großen Inventaren Docker-Timeout und Broker-Payloadlimit beachten.

Netzwerkdurchsatz ist die Differenz zweier Bytezähler geteilt durch die tatsächlich
verstrichene Zeit. Beim ersten Auftreten eines Interfaces wird nur die Basis erfasst;
Zählerresets liefern 0 statt eines Überlaufs. Es werden alle gemeldeten Interfaces
einschließlich Loopback und virtueller Interfaces erfasst.

Tailscale/NetBird benötigen CLI **und** Zugriff auf ihren laufenden lokalen Daemon.
Das Standardimage enthält beide CLIs nicht. Auf dem Host ausführen oder ein eigenes
Image mit passender CLI und Daemon-Socket verwenden. Fehlt das Programm beim Start,
wird nur dieses Modul deaktiviert und eine Warnung protokolliert. Nach Installation
den Agent neu starten. CLI-Fehler/ungültiges JSON lassen bestehende Sensoren ablaufen;
explizit gemeldete Offline-Zustände ergeben `OFF`. „Online Peers“ meint den von der
VPN-Software gemeldeten Zustand, keinen zusätzlichen aktiven Ping.

## MQTT und Home Assistant

```text
homeassistant/<component>/<AGENT_ID>/<sensor_key>/config   (QoS 1, retained)
<AGENT_ID>/<component>/<sensor_key>/state                  (QoS 1, nicht retained)
<AGENT_ID>/availability                                  (QoS 1, retained)
```

Jede Entity referenziert dasselbe Device mit `identifiers: [AGENT_ID]`, Anzeigename,
Hersteller `homelab-agent` und Build-Version. Sensorzustände sind JSON:

```json
{"value": 42.5, "attributes": {}}
```

Discovery enthält Templates für Zustand/Attribute und `expire_after` für **alle**
Sensoren. Binary-Sensoren verwenden `ON`/`OFF`. Namen von Pfaden, Interfaces und
Services werden als lesbarer Slug mit Hashsuffix abgebildet, um Kollisionen zu
vermeiden. MQTT-Sensor-IDs sind dauerhaft; Umbenennen eines Services erzeugt eine
neue Entity.

Der Agent erneuert Discovery bei jedem Messzyklus. So sind Broker-/HA-Neustarts
ohne spezielle Reihenfolge möglich. Messwerte sind absichtlich nicht retained:
alte Werte dürfen `expire_after` beim Wiederabspielen nicht neu starten. HA erhält
frische Messwerte spätestens im nächsten erfolgreichen Messzyklus. Discovery und
Availability bleiben retained.

MQTT-Verbindungen werden zunächst mit Backoff von 1 bis 30 Sekunden aufgebaut;
nach Verbindungsabbruch übernimmt der MQTT-Client den Reconnect mit maximal 30
Sekunden Backoff. Ein erfolgreicher Reconnect löst einen neuen Messzyklus aus.
Während Broker-Ausfällen werden keine alten Messwerte auf Platte gepuffert.
`online` wird nach dem Verbindungsaufbau und vor Veröffentlichung der Messwerte gesendet, beim geordneten Stop
`offline`; der Broker veröffentlicht bei unerwartetem Verbindungsverlust das LWT
`offline` (abhängig von TCP-Erkennung/Keepalive). Läuft nur ein Collector nicht mehr,
werden dessen Werte über `expire_after` unavailable.

### Entfernte Sensoren bereinigen

Deaktivierte Module oder umbenannte Checks löschen retained Discovery nicht
automatisch. Die bisherigen Entities werden unavailable. Zum Entfernen **des
gewünschten einzelnen Sensors** eine leere retained Nachricht an dessen bisheriges
Discovery-Topic senden, z. B.:

```sh
mosquitto_pub -h BROKER -t homeassistant/sensor/srv-01/cpu_percent/config -r -n
```

Broker-Zugangsdaten bei Bedarf ergänzen. Solange der Sensor aktiv ist, erzeugt der
Agent seine Discovery erneut. Bei einer Änderung der Agent-ID die alten Discovery-
Topics ebenfalls bereinigen.

### Beispiel-Dashboard

Neue Entities erhalten vorgeschlagene IDs wie `sensor.srv_01_cpu_percent`.
Home Assistant kann bestehende IDs beibehalten oder bei Kollisionen Suffixe vergeben;
die tatsächlichen IDs in der Entity-Liste prüfen und die Karte anpassen.

```yaml
type: entities
title: Server 1
entities:
  - entity: sensor.srv_01_cpu_percent
    name: CPU
  - entity: sensor.srv_01_ram_percent
    name: RAM
  - entity: sensor.srv_01_uptime
    name: Uptime
  - entity: sensor.srv_01_containers_running
    name: Laufende Container
```

Für Service-Sensoren und Disk-/Netzwerksensoren die tatsächlichen IDs mit Hashsuffix
aus HA auswählen. Die Container-Details sind Attribute von `containers_running`.

## Entwicklung und Releases

```sh
gofmt -w cmd internal
go mod verify
go vet ./...
go test -race -count=1 ./...
go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/homelab-agent-arm64 ./cmd/homelab-agent
```

Der Race Detector benötigt eine unterstützte C-Toolchain; unter Windows ohne diese
Toolchain `go test ./...` verwenden. Unit-Tests prüfen Config/Secrets, Zählerraten,
Docker-API/Inventar, HTTP/TCP, CLI-JSON und Discovery. Ein lokaler eingebetteter
MQTT-Testbroker prüft Publish, retained Discovery, LWT, Reconnect und Shutdown; er
wird nicht in das Agent-Binary eingebaut. Optional denselben Test mit
`MQTT_TEST_BROKER=tcp://127.0.0.1:1883 go test ./internal/mqtt -run TestBrokerIntegration`
gegen einen **isolierten Testbroker ohne Authentifizierung** ausführen. Der Test
verwendet die ID `integration` und schreibt Discovery/Availability auf diesem Broker.

CI führt Build, Vet, Tests mit Race Detector, einen Mosquitto-Test und Linux-Cross-
Builds aus; zusätzlich einen Windows-Build/Test und einen Container-Build. Tags wie
`v0.1.0` lösen den Multi-Arch-Build (`linux/amd64`, `linux/arm64`) mit Push nach
`ghcr.io/<owner>/<repo>` aus. Erst nach einem erfolgreichen Workflow existiert dieses
Image. Für öffentliche Nutzung ggf. die GHCR-Package-Sichtbarkeit auf public setzen.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Architektur: `internal/metric.Collector` verbindet unabhängige Collector-Pakete mit
MQTT. Docker verwendet ein mockbares API-Interface, CLI-Module ein Runner-Interface;
HTTP-Clients können im Test ersetzt werden. `cmd/homelab-agent` kümmert sich um
Konfiguration, Logging, Lebenszyklus und die parallelen Messzyklen.

### Quellen der Protokollimplementierung

- [Home Assistant MQTT Discovery](https://www.home-assistant.io/integrations/mqtt/#mqtt-discovery)
- [MQTT Sensor](https://www.home-assistant.io/integrations/sensor.mqtt/)
- [MQTT Binary Sensor](https://www.home-assistant.io/integrations/binary_sensor.mqtt/)
- [gopsutil](https://github.com/shirou/gopsutil)
- [Paho MQTT Go](https://github.com/eclipse-paho/paho.mqtt.golang)
- [NetBird CLI](https://docs.netbird.io/get-started/cli)

MIT-Lizenz, siehe [LICENSE](LICENSE).
