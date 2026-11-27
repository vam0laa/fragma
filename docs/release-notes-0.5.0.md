## v0.5.0: reliability and distribution

### Fixed
- **Linux:** a connection made straight to fragma's own port was relayed to itself without end,
  using up its open files and leaving the redirect rules behind on stop.
- **Linux:** `fragma dns on --doh` with a server given by name stopped all name lookups.
- **Windows:** a damaged settings file (after a power cut, say) left the proxy pointing at a port
  nobody served.
- **Windows:** restoring now also keeps what you changed while fragma ran, and tells you if it fails.
- **Windows:** the hosts file is backed up and replaced in one step, keeping its owner and
  permissions. The relay for apps listens only while `apps` is on; `stop` and `status` warn when
  entries are left.

### New
- `fragma report` on Linux and Windows: a report you can read and choose to share. Nothing is sent.
- A failed update brings the earlier version back (Linux and Windows).
- `install.sh`: `FRAGMA_VERSION`, root only for the install step when run without `sudo`,
  provenance checked with the GitHub CLI when possible.
- Releases are built by GitHub Actions, with `manifest.json`, checksums and provenance.

### Please read
- Windows support is still **experimental**. CI tests it on Windows; restarting and signing in, the
  administrator prompt, antivirus programs and Discord/Roblox on this version have not been tried on
  a real computer.
- The Windows program is not code-signed.

---

## v0.5.0: güvenilirlik ve dağıtım

### Düzeltilenler
- **Linux:** fragma'nın kendi portuna yapılan bir bağlantı sonsuz döngüye giriyor, durdururken
  yönlendirme kuralları kalabiliyordu.
- **Linux:** alan adıyla verilen DoH sunucusu tüm ad çözümlemeyi durduruyordu.
- **Windows:** bozulan ayar dosyası (örneğin elektrik kesintisi) interneti kesik bırakabiliyordu.
- **Windows:** geri alma, sen fragma çalışırken ne değiştirdiysen onu koruyor ve başarısız olursa
  haber veriyor.
- **Windows:** `hosts` dosyası yedekleniyor, tek adımda değiştiriliyor; sahibi ve izinleri korunuyor.

### Yenilikler
- Linux ve Windows'ta `fragma report`: okuyup istersen paylaştığın rapor. Hiçbir şey gönderilmez.
- Başarısız güncellemede eski sürüm geri gelir.
- `install.sh`: sürüm seçimi, yalnızca kurulum için root, mümkünse kaynak doğrulama.
- Sürümler GitHub Actions ile; `manifest.json`, sağlama toplamları ve kaynak kanıtı.

### Bilmen gerekenler
- Windows desteği hâlâ **deneysel**. Yeniden başlatma, yönetici onayı, antivirüs ve bu sürümde
  Discord/Roblox gerçek bir bilgisayarda denenmedi.
- Windows programı imzalı değil.
