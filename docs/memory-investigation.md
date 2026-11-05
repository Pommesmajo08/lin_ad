# Untersuchung des Server-Speicherverbrauchs

Im normalen Betrieb erstellten Geräte-Polling, Jobverwaltung und mehrere andere Verwaltungsaktionen vollständige Verzeichniskopien über JSON-Encoding und -Decoding. Dabei wurden auch unveränderte Skripte und Jobausgaben erneut allokiert. Die großen Encoding-Puffer und die erneut erzeugten Texte verstärkten Speicherbedarf und GC-Last.

Die neuen Snapshots kopieren Maps, veränderliche Slices und Zeigerwerte; unveränderliche Strings werden gemeinsam genutzt. Änderungen an einem Snapshot beeinflussen den gespeicherten Zustand nicht. Bestehende Tests für fehlgeschlagene Speicherung sowie ein neuer Isolationstest prüfen das.

## Messung

Fixture: 2.000 abgeschlossene Jobläufe, je etwa 16 KB Skript und 12 KB Ausgabe. Go 1.27.3, Linux/AMD64. Die Messung erfasst temporäre Allokationen, nicht den maximalen Prozess-RSS.

| Verfahren | Allokationen pro Snapshot | Laufzeit pro Snapshot |
| --- | ---: | ---: |
| JSON-Roundtrip | 453.745.746 Bytes | 231 ms |
| Strukturierter Snapshot | 751.664 Bytes | 0,56 ms |

Die JSON-Werte schwanken durch Pool-Wiederverwendung und GC; ein früherer Lauf zeigte etwa 228 MB statt 454 MB. Beide Werte liegen weit über dem neuen Snapshot.

Ein zusätzlicher Test ruft den tatsächlichen Geräte-Claim-Endpunkt 500-mal auf. Mit derselben gefüllten Historie lagen die Allokationen pro Poll bei 0,73 MiB und der Go-Heap nach Speicherbereinigung bei 55,53 MiB sowohl nach 100 als auch nach 500 Polls. In diesem Pfad war nach der Änderung kein fortlaufendes Heap-Wachstum messbar. Das ist kein vollständiger Langzeitnachweis für alle Serverfunktionen.

Reproduzieren:

```sh
go test ./internal/directory -run 'TestSnapshotIsolation|TestIdlePollingMemoryBounded' -v -count=1
go test ./internal/directory -run '^$' -bench BenchmarkDirectorySnapshot -benchmem -benchtime=3x
```

Das Docker-Image setzt zusätzlich `GOMEMLIMIT=512MiB`. Dieses weiche Go-Budget ist keine harte RSS- oder Containergrenze. Eine optionale harte Containergrenze und Diagnosebefehle stehen in der Installationsanleitung.

Ohne OOM-Killer-Meldung, tatsächlichen Containerverbrauch und Angaben zum Hyper-V-Host kann nicht bestätigt werden, dass diese Allokationen allein den gemeldeten Ausfall einer VM mit knapp 16 GB RAM verursacht haben. Ein unbegrenztes Speicherleck wurde durch diese Untersuchung nicht nachgewiesen.
