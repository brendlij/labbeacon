# homelab-agent

Ein kleines Go-Binary pro Server: Systemmetriken, Docker-Inventar und Service-Checks
per MQTT an Home Assistant, mit optionalen Steuerungsbuttons ab 0.2.0. Jeder Agent erscheint durch MQTT Discovery als eigenes
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
mit `:ro` verhindert **keine schreibenden Docker-API-Aufrufe**. Im Standardbetrieb
nutzt der Agent nur GET-Requests. Erst `control_containers.enabled: true` schaltet
POST-Aufrufe für start/stop/restart frei. Socket-Zugriff ist eine weitreichende Hostberechtigung.

## Schnellstart als Binary

```sh
go build -trimpath -ldflags="-s -w -X homelab-agent/internal/version.Version=0.2.0" -o bin/homelab-agent ./cmd/homelab-agent
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
Check-Typen und fehlende Pflichtwerte werden beim Start abgelehnt. Datenmodule sind
im Code standardmäßig deaktiviert, außer den eigenen Versions-/Uptime-Sensoren;
die Beispieldateien aktivieren passende Module. Alle Steuerungsfunktionen sind deaktiviert.

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
ist in 0.2.0 nicht implementiert; dadurch werden keine Raw-Socket-Rechte benötigt.
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
`v0.2.0` lösen den Multi-Arch-Build (`linux/amd64`, `linux/arm64`) mit Push nach
`ghcr.io/<owner>/<repo>` aus. Erst nach einem erfolgreichen Workflow existiert dieses
Image. Für öffentliche Nutzung ggf. die GHCR-Package-Sichtbarkeit auf public setzen.

```sh
git tag v0.2.0
git push origin v0.2.0
```

Architektur: `internal/module.Module` besitzt `Name()`, `Enabled()` und `Collect(ctx)`.
`module.Registration` bindet die bestehenden Collector-Pakete an diese Schnittstelle;
die Registry erledigt parallele Sammlung und Fehlerisolation. Ein neues Package
benötigt nur einen Collector und einen Registrierungseintrag in `registered()`.
Optionale `control.Provider` liefern dynamische Aktionen über `Actions(ctx)`.
`control.Action` besitzt `ID()`, `Execute(ctx)` und `RequiresConfirm()`; `control.Entry`
ergänzt Name, Modul, Vorprüfung und Availability-Übergang. Die Main-Loop serialisiert
Steuerung und Messzyklen. Docker verwendet mockbare API-Interfaces, CLI-Module einen
Runner und HTTP-Module austauschbare Clients.

## Verfügbare Module

Die Defaults beziehen sich auf Code-Defaults; Beispielconfigs aktivieren Systemmetriken.

| Modul / Feature | Config-Schalter | Default | Benötigte Rechte / Voraussetzungen |
| --- | --- | --- | --- |
| System-Basis | `modules.system.enabled` | disabled | Lesbare Systeminformationen / Host-Mounts im Container |
| CPU-Temperatur | `modules.system.temperature` | disabled | Lesbare hwmon-/Thermal-Sensoren, optional `HOST_SYS` |
| Top-Prozesse + Prozesszahl | `modules.system.processes` | disabled | Prozessdaten lesen; nicht lesbare Prozesse werden gekennzeichnet |
| Offene FDs + Prozesszahl | `modules.system.file_descriptors` | disabled | Zugriff auf Prozess-FD-Informationen; Linux bevorzugt |
| Boot-Timestamp | `modules.system.boot_time` | disabled | Host-Bootinformationen |
| Docker-Inventar + Restart-Count | `modules.docker.enabled` | disabled | Docker-Socket: GET `/containers/*` |
| Docker CPU/RAM | `modules.docker.stats` | disabled | Zusätzlich GET `/containers/*/stats` |
| Image-Update-Digest | `modules.docker.image_updates.enabled` | disabled | GET `/images/*`, HTTPS-Zugriff auf anonyme Registry |
| HTTP/TCP-Checks | `modules.services.enabled` | disabled | Netzwerkzugriff zu den Zielen |
| Tailscale / NetBird | jeweiliges `enabled` | disabled | CLI und lokaler Daemon erreichbar |
| Netzwerk-IP-Adressen | `modules.network.enabled` | disabled | Netzwerkschnittstellen lesbar |
| Lokale IPs | `modules.network.local_ips` | enabled innerhalb Netzwerkmodul | Im Container Host-Netzwerk für Host-IPs |
| Öffentliche IP | `modules.network.public_ip.enabled` | disabled | Ausgehendes HTTPS zum konfigurierten Endpoint |
| Agent-Version/Uptime | `agent.metrics_enabled` | enabled | Keine zusätzlichen Rechte |

Alle System-Erweiterungen benötigen zusätzlich `modules.system.enabled: true`.
`top_n` begrenzt die beiden Attributlisten `top_cpu` und `top_ram` (Standard 5,
zulässig 1–100). Es gibt einen Summary-Sensor, keine Entity pro Prozess. CPU-Raten
verwenden die Differenz zweier Prozessmessungen; die erste CPU-Liste ist daher leer.
100 % entspricht einem logischen Kern, mehrkernige Prozesse können darüber liegen.
PID-Wiederverwendung wird anhand der Prozess-Startzeit erkannt. RAM ist RSS in Bytes.
Dateideskriptoren sind die Summe über lesbare Prozesse; Attribute `partial`,
`observed_processes` und `total_processes` zeigen eingeschränkte Sichtbarkeit an.
Der Agent eskaliert keine Rechte für Prozessmetriken. `hidepid` und Container-
Namespaces können bereits die sichtbare Prozessliste einschränken.

`cpu_temperature` zeigt den höchsten erkannten CPU-Sensorwert; Einzelwerte liegen
als Attribute vor. Unter Linux wird bei Bedarf `/sys/class/thermal` bzw. `HOST_SYS`
verwendet. Ohne lesbaren CPU-Sensor erscheint kein erfundener Wert. `boot_time` ist
ein UTC-Timestamp mit HA-Geräteklasse `timestamp`; `agent_uptime` zählt unabhängig
davon seit Prozessstart.

Container-Stats ergänzen die Inventarattribute und erzeugen CPU-/RAM-Sensoren je
Container. CPU verwendet Docker-CPU-/Systemzeitdifferenzen und Online-Kernanzahl;
RAM zieht `inactive_file` bzw. `total_inactive_file` ab. Ohne gültiges CPU-Zeitpaar
wird kein CPU-Wert gesendet. Stats werden nur für laufende Container abgefragt.
`restart_count` stammt aus Docker Inspect und ist der Docker-eigene Zähler; er ist
kein vollständiges Audit aller manuellen Stop/Start-Vorgänge. Stats können pro
Container etwa eine Sekunde benötigen: bei vielen Containern `docker.timeout`,
`poll_interval` und `expire_after` entsprechend erhöhen. Fehler einer Stats-Abfrage
verwerfen nicht das zuvor gelesene Inventar.

Image-Updates werden standardmäßig höchstens alle `6h` mit `5s` Timeout geprüft.
Verglichen werden lokale RepoDigests mit einem anonym per HTTPS gelesenen
Schema-2-/OCI-Manifest und ggf. dessen Plattform-Deskriptoren. Ein lokaler Digest in
einer Manifestliste bedeutet „kein Update für dieses Image“. Authentifizierung,
Bearer-Token-Flows und Credentials werden nicht verwendet: 401/403/404, ungetaggte
Image-IDs, digest-gepinnte Referenzen oder fehlende RepoDigests werden übersprungen.
Das betrifft auch öffentliche Registries, die einen anonymen Bearer-Token verlangen
(häufig Docker Hub/GHCR). Der Agent zieht oder aktualisiert keine Images.

Das Netzwerkmodul veröffentlicht pro Interface einen Sensor mit Anzahl der Adressen
und vollständiger CIDR-Liste in `addresses`. `public_ip.endpoint` ist standardmäßig
`https://api.ipify.org`, `interval: 15m`, `timeout: 5s`; Intervalle unter einer Minute
sind unzulässig. Der Endpoint muss eine einzelne IP als Text liefern. Der Cache
wird zwischen Polls wiederverwendet; `checked_at` enthält den letzten erfolgreichen
Abruf. Nach einem fehlgeschlagenen Refresh wird kein alter Wert weiterveröffentlicht;
HA lässt den Sensor ablaufen. Der nächste Versuch erfolgt nach dem Cacheintervall.

## Verfügbare Steuerungsaktionen

| Aktion | Modul / Freigabe | Default | Benötigte Berechtigungen |
| --- | --- | --- | --- |
| Host reboot | `host_control.enabled` + `reboot.enabled` | disabled | Nativer Linux-systemd-Host, root, Systembus/Manager und konfiguriertes Programm |
| Host shutdown | `host_control.enabled` + `shutdown.enabled` | disabled | Wie Reboot |
| Container start/stop/restart | `modules.docker.control_containers.enabled` | disabled | Linux-Docker-Socket mit GET- und passenden POST-Rechten |
| Service start/stop/restart | je Check `allow_control: true` + `systemd_unit` | disabled | Nativer Linux-systemd-Host, root, geladene exakte `.service`-Unit |
| Agent Restart | `agent_control.enabled` | disabled | Supervisor mit Restart-on-failure oder Docker-Restart-Policy |

Die Standard-systemd-Unit läuft weiter als unprivilegierter Benutzer und ermöglicht
keine Host-/Service-Steuerung. Für diese Funktionen muss der Administrator die Unit
bewusst anpassen (z. B. systemd-Drop-in mit `User=root` und `Group=root`). Der Agent
ruft weder sudo noch interaktive Polkit-Abfragen auf. Host-/systemd-Steuerung wird
unter Windows und in erkannten Containern deaktiviert; ein bloßes `pid: host` oder
ein gemounteter Socket aktiviert sie nicht. Docker-Containersteuerung funktioniert
hingegen im normalen Agent-Container bei entsprechendem Socketzugriff.

Vor der Registrierung werden Programme, Host/systemd-Voraussetzungen, geladene
Units bzw. Socket-Erreichbarkeit read-only geprüft. Vor jeder Ausführung erfolgen
erneute Unit-/Zielprüfungen. Unverfügbare Aktionen werden geloggt. Docker-Authorization-
Plugins oder sich ändernde Systembus-Richtlinien können GET erlauben und POST später
ablehnen; eine garantiert vollständige Schreibrechteprüfung wäre selbst eine Mutation.
Solche Fehler werden bei Ausführung strukturiert protokolliert, ohne automatischen
Retry. Timeouts können bedeuten, dass eine bereits angenommene Aktion dennoch läuft.

### Freigaben konfigurieren

```yaml
host_control:
  enabled: false
  timeout: 15s
  reboot:
    enabled: false
    command: [systemctl, --no-ask-password, reboot]
    preflight: [systemctl, --no-ask-password, show, --property=Version, --value]
    confirm_required: true
  shutdown:
    enabled: false
    command: [systemctl, --no-ask-password, poweroff]
    preflight: [systemctl, --no-ask-password, show, --property=Version, --value]
    confirm_required: true
agent_control:
  enabled: false
```

`command` und `preflight` sind Argumentlisten ohne implizite Shell; `preflight` muss
ein **nur lesender** Test sein. Beide kommen ausschließlich aus der vertrauenswürdigen
lokalen Config, nie aus einem MQTT-Payload. Beispiel für gezielte Containerfreigabe
innerhalb des bereits aktivierten Docker-Moduls:

```yaml
control_containers:
  enabled: true
  allow: [herbst, adguard]
  deny: [postgres]
```

Eine leere Allowlist erlaubt nach Aktivierung alle Namen; Deny hat immer Vorrang.
Es sind exakte Docker-Namen ohne führenden Slash, keine Globmuster. Die Buttons
verwenden unveränderliche Container-IDs als Ziele; eine neue Instanz mit gleichem
Namen erhält neue Buttons. Ein Namenswechsel nach Discovery wird vor Ausführung
abgelehnt, bis das Inventar aktualisiert wurde.

Für einen Service-Check `systemd_unit: adguard.service` und `allow_control: true`
ergänzen. Service-Name und Unit sind getrennte Felder. Die Unit darf keine Optionen,
Pfade oder Wildcards enthalten. Docker-/Service-Aktionen haben ein 30s-Zeitbudget,
Hostaktionen `host_control.timeout`. Docker stop/restart verwendet 10s Grace-Zeit.
Erfolgreiche Aktionen lösen sofort einen neuen Mess-/Discovery-Zyklus aus.

Agent Restart beendet den Prozess nach MQTT-Offline und Disconnect mit **Exitcode
75**. Die mitgelieferten systemd-/Compose-Restart-Regeln starten ihn erneut und lesen
die Config neu. Ohne Supervisor bleibt der Prozess beendet. Eine geänderte, ungültige
Config verhindert den Neustart; deshalb vorher `-check-config` verwenden.

### MQTT-Kommandos und Audit

Button-Discovery: `homeassistant/button/<ID>/<action_id>/config`.
Command-Topic: `<ID>/button/<action_id>/command`, QoS 0, **nicht retained**.
Buttons teilen das Monitoring-Device; `expire_after` gilt nur für Sensoren,
Button-Verfügbarkeit über das gemeinsame Availability-Topic.

```json
{"session":"aktueller-Wert-aus-Discovery","confirm":false}
```

Der Agent rotiert `session` bei MQTT-Verbindungswechseln und nach jedem begonnenen
Steuerungsversuch, auch bei Ausführungsfehlern. Die aktuelle Session steht im Discovery-`payload_press`, im
Attribut `control_session` des Agent-Version-Sensors und als JSON unter
`<ID>/control/session`. Sie ist ein Schutz gegen veraltete Nachrichten, **kein
geheimes Authentifizierungsmerkmal**. Retained-Replays, DUP-Pakete, ungültiges JSON,
Payloads über 512 Bytes, alte Sessions und Befehle ohne freigeschaltete Aktion werden
verworfen. Die Queue fasst acht Befehle; nach 15s Wartezeit verfallen sie. Gleiche
Aktionen haben 2s Cooldown. Befehle und Messzyklen werden serialisiert.

Audit-Logs enthalten Zeit, Aktion, Topic, QoS, Beginn und Ergebnis/Ablehnungsgrund.
MQTT 3.1.1 leitet keine Identität des Publishers an Subscriber weiter; `actor` wird
deshalb ehrlich als unbekannt ausgewiesen. Authentifizierung/Autorisierung muss am
Broker erfolgen: nur vertrauenswürdigen HA-/Admin-Clients Schreibrechte auf
`<ID>/button/+/command` geben, TLS einsetzen und Broker-Logs für Benutzerzuordnung
verwenden. Der Agent benötigt Schreibrechte auf eigene State-/Discovery-Topics,
Leserechte auf eigene Button-Discovery zur Bereinigung und bei Steuerung auf
eigene Command-Topics. Bestätigung ersetzt diese ACLs nicht.

Vor Hostaktionen wird `rebooting` bzw. `shutting_down` auf Availability bestätigt
publiziert. Bei einem Befehlsfehler wird `online` wiederhergestellt; beim erfolgreichen
Herunterfahren des Agent folgt `offline`. HA-MQTT-Topic-Überwachung zeigt die
Übergangsnachrichten; ein automatischer HA-Logbook-Eintrag ist dadurch nicht garantiert.

Veraltete **Button**-Discovery wird automatisch bereinigt, auch nach Neustarts mit
deaktivierter Steuerung (ggf. im folgenden Poll). Sensor-Discovery bleibt gemäß dem
oben beschriebenen manuellen Bereinigungsverfahren erhalten. Docker-Inventarfehler
nehmen Docker-Buttons vorsichtshalber vorübergehend aus der Discovery.

### Bestätigung gefährlicher Aktionen in Home Assistant

**MQTT-Discovery hat kein `confirmation`-Feld.** Bestätigungsdialoge sind eine
[Dashboard-Aktion](https://www.home-assistant.io/dashboards/actions), keine Eigenschaft
der [MQTT-Button-Integration](https://www.home-assistant.io/integrations/button.mqtt/).
`confirm_required: true` wird deshalb im Agent durchgesetzt: der normale
`button.press`-Payload bestätigt nichts und wird abgelehnt. Verwende stattdessen
dieses HA-Script in `scripts.yaml` (ID/Entity-Namen anpassen, `agent.metrics_enabled`
aktiv lassen):

```yaml
homelab_srv01_reboot:
  alias: Server 1 neu starten
  mode: single
  sequence:
    - action: mqtt.publish
      data:
        topic: srv-01/button/host_reboot/command
        qos: 0
        retain: false
        payload: >-
          {{ {'session': state_attr('sensor.srv_01_agent_version', 'control_session'),
              'confirm': true} | to_json }}
```

Dashboard-Karte mit nativem Bestätigungsdialog:

```yaml
type: button
name: Server 1 neu starten
icon: mdi:restart
tap_action:
  action: perform-action
  perform_action: script.homelab_srv01_reboot
  confirmation:
    text: Server 1 wirklich neu starten?
hold_action:
  action: none
```

Für Shutdown analog `host_shutdown` verwenden. Die Bestätigung gilt für diese
Dashboard-Karte; direkte Script-/MQTT-Aufrufe können keinen menschlichen Klick
beweisen und müssen über HA-Rechte/Broker-ACLs geschützt werden. Mit
`confirm_required: false` funktioniert der entdeckte Button direkt ohne Dialog.
Der Agent gibt niemals automatisch `confirm: true` in Discovery vor.

### Quellen der Protokollimplementierung

- [Home Assistant MQTT Discovery](https://www.home-assistant.io/integrations/mqtt/#mqtt-discovery)
- [MQTT Sensor](https://www.home-assistant.io/integrations/sensor.mqtt/)
- [MQTT Binary Sensor](https://www.home-assistant.io/integrations/binary_sensor.mqtt/)
- [gopsutil](https://github.com/shirou/gopsutil)
- [Paho MQTT Go](https://github.com/eclipse-paho/paho.mqtt.golang)
- [NetBird CLI](https://docs.netbird.io/get-started/cli)

MIT-Lizenz, siehe [LICENSE](LICENSE).
