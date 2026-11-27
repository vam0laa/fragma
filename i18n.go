package main

import (
	"fmt"
	"os"
	"strings"
)

// lang is "tr" or "en". Chosen from FRAGMA_LANG, then the usual locale
// variables (LC_ALL, LC_MESSAGES, LANG), then the system's display language;
// anything that is not Turkish is English.
var lang = detectLang()

func detectLang() string {
	for _, k := range []string{"FRAGMA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "tr") {
				return "tr"
			}
			return "en"
		}
	}
	return systemLocale()
}

// systemLocale is asked when no variable says anything (the normal case on
// Windows). A variable so that tests do not depend on the machine.
var systemLocale = osLocale

// T returns the message for key in the active language, formatted with args.
// It falls back to English, then to the key itself.
func T(key string, args ...any) string {
	s, ok := catalog[lang][key]
	if !ok {
		if s, ok = catalog["en"][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

var catalog = map[string]map[string]string{
	"en": {
		"os.unsupported":  "this command is only available on Linux. On this system use: fragma proxy",
		"fail.nohome":     "cannot find your user config folder",
		"fail.root":       "this needs root: sudo fragma %s",
		"fail.precheck":   "this system cannot run fragma, nothing was changed: %v",
		"fail.notready":   "it did not finish starting, see: journalctl -u fragma -n 30",
		"upd.failed":      "the new version did not work: %v.",
		"upd.restored":    "The previous version is back and running.",
		"upd.restoredoff": "The previous version is back; it stays switched off, as before.",
		"upd.removed":     "Nothing was left installed.",
		"upd.broken":      "The earlier state could not be restored completely: %v",
		"step.kept":       "Kept",
		"fail.copy":       "installing the binary: %v",
		"fail.unit":       "writing the service file: %v",
		"fail.reload":     "systemctl daemon-reload failed (is this a systemd system?)",
		"fail.enable":     "could not enable the service, see: journalctl -u fragma",
		"fail.start":      "could not start the service, see: journalctl -u fragma",
		"fail.stayup":     "the service did not stay up, see: journalctl -u fragma -n 30",
		"fail.ctl":        "systemctl %s fragma failed (installed? run the install command first)",
		"fail.addusage":   "usage: sudo fragma add example.com [more.org ...]",
		"fail.badhost":    "not a host name: %q",
		"fail.dnsusage":   "usage: sudo fragma dns on [--doh URL] | off | status",
		"fail.restart":    "could not restart the service, see: journalctl -u fragma",
		"step.installed":  "Installed",
		"step.service":    "Service",
		"step.running":    "fragma",
		"step.removed":    "Removed",
		"step.known":      "Known",
		"step.added":      "Added",
		"step.dns":        "DNS",
		"d.enabled":       "enabled, starts at every boot",
		"d.running":       "running",
		"d.removed":       "service, rules and program are gone",
		"d.applies":       "applies within 10 seconds",
		"d.dnson":         "answered over DNS-over-HTTPS; your lookups go to that provider, not to your ISP",
		"d.dnsoff":        "untouched again",
		"word.on":         "ON",
		"word.off":        "OFF",
		"box.title":       "fragma is ON",
		"box.l1":          "Discord, Roblox and other blocked sites",
		"box.l2":          "should open now. Every other site is",
		"box.l3":          "left exactly as it is.",
		"box.stop":        "turn it off",
		"box.start":       "turn it on again",
		"box.status":      "is it on?",
		"box.add":         "add a blocked site",
		"box.uninstall":   "remove everything",
		"ctl.on":          "fragma is %s.",
		"ctl.off":         "fragma is %s. Nothing is redirected anymore.",
		"st.on":           "fragma is %s  %s",
		"st.off":          "fragma is %s  %s",
		"st.onhint":       "(stop with: sudo fragma stop)",
		"st.offhint":      "(start with: sudo fragma start)",
		"list.builtin":    "Built in",
		"list.always":     "always",
		"list.yours":      "Yours",
		"list.learned":    "Learned",
		"list.found":      "found blocked by fragma itself",
		"list.none":       "(none)",
		"list.footer":     "Every other site is passed through unchanged.",
		"dns.on":          "DNS-over-HTTPS is %s",
		"dns.off":         "DNS is %s (your system DNS is used as is)",
		"dns.untouched":   "untouched",
		"win.appsusage":   "usage: fragma apps on | off | status",
		"win.appsleft":    "fragma is not running, but the hosts file still points Discord and Roblox names at this computer: they will not open. Start fragma, or run: fragma apps off",
		"win.appsfail":    "the hosts file was not changed (was the administrator prompt declined?)",
		"win.appsbackup":  "the hosts file from before this change: %s",
		"win.appson":      "apps that ignore the proxy (Discord updater): ON. fragma must be running.",
		"win.appsoff":     "apps that ignore the proxy: OFF (hosts file has no fragma entries)",
		"win.already":     "fragma is already running",
		"win.foreign":     "another proxy or setup script is already configured in Windows; fragma will not overwrite it. Turn it off first (Settings > Network & internet > Proxy)",
		"win.running":     "fragma is running on %s (%s). Press Ctrl+C to stop.",
		"win.dnson":       "DNS-over-HTTPS",
		"win.dnsoff":      "system DNS",
		"win.stopped":     "fragma stopped: your proxy settings are restored",
		"win.restorefail": "could not restore the proxy settings: %v (check Settings > Network & internet > Proxy)",
		"win.statelost":   "the saved proxy settings were damaged (kept as %s). fragma turned its proxy off; if you had proxy settings of your own, check Settings > Network & internet > Proxy",
		"win.statebad":    "the saved proxy settings were damaged (kept as %s). The proxy in use is not fragma's and was not changed; check Settings > Network & internet > Proxy",
		"win.startfail":   "fragma did not start: %s",
		"win.updnothing":  "nothing was changed: %v",
		"win.updfailed":   "the new version did not work: %v.",
		"win.updstopped":  "The earlier settings are back, but fragma is not running: start it again.",
		"win.stopfail":    "could not stop the running fragma",
		"win.off":         "fragma is OFF. Your proxy settings are restored.",
		"win.onhint":      "(stop with: fragma stop)",
		"win.offhint":     "(start with: fragma start)",
		"win.leftover":    "old proxy settings were not restored yet, run: fragma stop",
		"win.autostart":   "starts every time you sign in",
		"win.manage":      "To control it later (PowerShell):",
		"win.removed":     "autostart and program are gone, proxy settings restored",
		"win.nodns":       "on Windows use: fragma stop, then fragma start --dns (or: fragma install --dns)",
		"usage.win": `fragma %s: get around SNI-based access blocks (Discord, Roblox, ...)

  fragma install          install and start (also starts at every sign-in)
  fragma start            turn it on (sets the Windows proxy)
  fragma stop             turn it off (restores your proxy settings)
  fragma status           is it on?
  fragma uninstall        remove it
  fragma apps on|off      for apps that ignore the proxy, e.g. Discord (asks for admin)

  fragma add <site>       also treat this site as blocked
  fragma list             which sites does fragma change? (all others: none)

  fragma probe            how is each site blocked on this connection?
  fragma experiment       which technique does your ISP's DPI not understand?
  fragma run              like start, but in this window
  fragma proxy            only the local proxy, Windows settings untouched

No administrator rights needed except for apps on|off. Option for install/start/run: --dns (DNS-over-HTTPS)
Language: FRAGMA_LANG=tr or en (default: Windows display language)
`,
		"usage.other": `fragma %s (experimental on this system): get around SNI-based access blocks (Discord, Roblox, ...)

  fragma proxy            start the local HTTPS proxy, then set 127.0.0.1:8899 as your proxy
  fragma add <site>       also treat this site as blocked
  fragma list             which sites does fragma change? (all others: none)

  fragma probe            how is each site blocked on this connection?
  fragma experiment       which technique does your ISP's DPI not understand?

System-wide mode (install/stop/start) exists on Linux and Windows only.
Language: FRAGMA_LANG=tr or en (default: from your locale)
`,
		"usage": `fragma %s: get around SNI-based access blocks (Discord, Roblox, ...)

  sudo fragma install     install and start (also starts at every boot)
  sudo fragma stop        turn it off
  sudo fragma start       turn it on again
  fragma status           is it on?
  sudo fragma uninstall   remove everything

  sudo fragma add <site>  also treat this site as blocked
  fragma list             which sites does fragma change? (all others: none)
  sudo fragma dns on|off  optional: answer DNS over DNS-over-HTTPS (off by default)

  fragma probe            how is each site blocked on this connection?
  fragma experiment       which technique does your ISP's DPI not understand?
  fragma proxy            local HTTPS proxy, no root (per app)
  sudo fragma run         system-wide mode in the foreground (the service uses this)

Language: FRAGMA_LANG=tr or en (default: from your locale)
`,
	},
	"tr": {
		"os.unsupported":  "bu komut yalnızca Linux'ta var. Bu sistemde şunu kullan: fragma proxy",
		"fail.nohome":     "kullanıcı ayar klasörün bulunamadı",
		"fail.root":       "bunun için yönetici yetkisi gerekir: sudo fragma %s",
		"fail.precheck":   "bu sistem fragma'yı çalıştıramıyor, hiçbir şey değiştirilmedi: %v",
		"fail.notready":   "başlaması tamamlanmadı, bak: journalctl -u fragma -n 30",
		"upd.failed":      "yeni sürüm çalışmadı: %v.",
		"upd.restored":    "Önceki sürüm geri kondu ve çalışıyor.",
		"upd.restoredoff": "Önceki sürüm geri kondu; eskisi gibi kapalı duruyor.",
		"upd.removed":     "Hiçbir şey kurulu bırakılmadı.",
		"upd.broken":      "Eski durum tam olarak geri getirilemedi: %v",
		"step.kept":       "Saklandı",
		"fail.copy":       "program kurulamadı: %v",
		"fail.unit":       "servis dosyası yazılamadı: %v",
		"fail.reload":     "systemctl daemon-reload başarısız (sistem systemd tabanlı mı?)",
		"fail.enable":     "servis etkinleştirilemedi, bak: journalctl -u fragma",
		"fail.start":      "servis başlatılamadı, bak: journalctl -u fragma",
		"fail.stayup":     "servis ayakta kalmadı, bak: journalctl -u fragma -n 30",
		"fail.ctl":        "systemctl %s fragma başarısız (kurulu mu? önce kurulum komutunu çalıştır)",
		"fail.addusage":   "kullanım: sudo fragma add example.com [diger.org ...]",
		"fail.badhost":    "geçerli bir site adı değil: %q",
		"fail.dnsusage":   "kullanım: sudo fragma dns on [--doh URL] | off | status",
		"fail.restart":    "servis yeniden başlatılamadı, bak: journalctl -u fragma",
		"step.installed":  "Kuruldu",
		"step.service":    "Servis",
		"step.running":    "fragma",
		"step.removed":    "Kaldırıldı",
		"step.known":      "Biliniyor",
		"step.added":      "Eklendi",
		"step.dns":        "DNS",
		"d.enabled":       "etkin, her açılışta başlar",
		"d.running":       "çalışıyor",
		"d.removed":       "servis, kurallar ve program kaldırıldı",
		"d.applies":       "10 saniye içinde geçerli olur",
		"d.dnson":         "DNS-over-HTTPS ile yanıtlanıyor; aramaların operatörüne değil o sağlayıcıya gider",
		"d.dnsoff":        "yeniden dokunulmuyor",
		"word.on":         "AÇIK",
		"word.off":        "KAPALI",
		"box.title":       "fragma AÇIK",
		"box.l1":          "Discord, Roblox ve engelli diğer siteler",
		"box.l2":          "şimdi açılmalı. Diğer tüm siteler",
		"box.l3":          "olduğu gibi bırakılır.",
		"box.stop":        "kapat",
		"box.start":       "tekrar aç",
		"box.status":      "açık mı?",
		"box.add":         "engelli site ekle",
		"box.uninstall":   "her şeyi kaldır",
		"ctl.on":          "fragma %s.",
		"ctl.off":         "fragma %s. Artık hiçbir şey yönlendirilmiyor.",
		"st.on":           "fragma %s  %s",
		"st.off":          "fragma %s  %s",
		"st.onhint":       "(durdurmak için: sudo fragma stop)",
		"st.offhint":      "(başlatmak için: sudo fragma start)",
		"list.builtin":    "Yerleşik",
		"list.always":     "her zaman",
		"list.yours":      "Senin",
		"list.learned":    "Öğrenilen",
		"list.found":      "fragma'nın kendi bulduğu engelli siteler",
		"list.none":       "(yok)",
		"list.footer":     "Diğer tüm siteler olduğu gibi geçer.",
		"dns.on":          "DNS-over-HTTPS %s",
		"dns.off":         "DNS %s (sistem DNS'in olduğu gibi kullanılıyor)",
		"dns.untouched":   "dokunulmuyor",
		"win.appsusage":   "kullanım: fragma apps on | off | status",
		"win.appsleft":    "fragma çalışmıyor ama hosts dosyası Discord ve Roblox adlarını hâlâ bu bilgisayara yönlendiriyor: açılmazlar. fragma'yı başlat ya da şunu çalıştır: fragma apps off",
		"win.appsfail":    "hosts dosyası değiştirilmedi (yönetici onayı reddedildi mi?)",
		"win.appsbackup":  "hosts dosyasının bu değişiklikten önceki hali: %s",
		"win.appson":      "proxy kullanmayan uygulamalar (Discord güncelleyicisi): AÇIK. fragma çalışıyor olmalı.",
		"win.appsoff":     "proxy kullanmayan uygulamalar: KAPALI (hosts dosyasında fragma kaydı yok)",
		"win.already":     "fragma zaten çalışıyor",
		"win.foreign":     "Windows'ta başka bir proxy veya kurulum betiği ayarlı; fragma üzerine yazmaz. Önce onu kapat (Ayarlar > Ağ ve internet > Proxy)",
		"win.running":     "fragma %s üzerinde çalışıyor (%s). Durdurmak için Ctrl+C.",
		"win.dnson":       "DNS-over-HTTPS",
		"win.dnsoff":      "sistem DNS'i",
		"win.stopped":     "fragma durdu: proxy ayarların geri yüklendi",
		"win.restorefail": "proxy ayarları geri yüklenemedi: %v (Ayarlar > Ağ ve internet > Proxy'yi kontrol et)",
		"win.statelost":   "kayıtlı proxy ayarları bozulmuş (%s olarak saklandı). fragma kendi proxy'sini kapattı; kendi proxy ayarın vardıysa Ayarlar > Ağ ve internet > Proxy bölümünü kontrol et",
		"win.statebad":    "kayıtlı proxy ayarları bozulmuş (%s olarak saklandı). Kullanılan proxy fragma'nın değil, değiştirilmedi; Ayarlar > Ağ ve internet > Proxy bölümünü kontrol et",
		"win.startfail":   "fragma başlatılamadı: %s",
		"win.updnothing":  "hiçbir şey değiştirilmedi: %v",
		"win.updfailed":   "yeni sürüm çalışmadı: %v.",
		"win.updstopped":  "Önceki ayarlar geri geldi ama fragma çalışmıyor: yeniden başlat.",
		"win.stopfail":    "çalışan fragma durdurulamadı",
		"win.off":         "fragma KAPALI. Proxy ayarların geri yüklendi.",
		"win.onhint":      "(durdurmak için: fragma stop)",
		"win.offhint":     "(başlatmak için: fragma start)",
		"win.leftover":    "eski proxy ayarları henüz geri yüklenmedi, çalıştır: fragma stop",
		"win.autostart":   "her oturum açışında başlar",
		"win.manage":      "Sonradan yönetmek için (PowerShell):",
		"win.removed":     "otomatik başlatma ve program kaldırıldı, proxy ayarları geri yüklendi",
		"win.nodns":       "Windows'ta şunu kullan: fragma stop, sonra fragma start --dns (veya: fragma install --dns)",
		"usage.win": `fragma %s: SNI tabanlı erişim engellerini aşar (Discord, Roblox, ...)

  fragma install          kur ve başlat (her oturum açışında da başlar)
  fragma start            aç (Windows proxy'sini ayarlar)
  fragma stop             kapat (proxy ayarlarını geri yükler)
  fragma status           açık mı?
  fragma uninstall        kaldır
  fragma apps on|off      proxy kullanmayan uygulamalar için, örn. Discord (yönetici sorar)

  fragma add <site>       bu siteyi de engelli say
  fragma list             fragma hangi siteleri değiştiriyor? (diğer hepsi: hiçbiri)

  fragma probe            bu bağlantıda her site nasıl engelleniyor?
  fragma experiment       operatörünün DPI'ı hangi tekniği anlamıyor?
  fragma run              start gibi, ama bu pencerede
  fragma proxy            yalnızca yerel proxy, Windows ayarlarına dokunmaz

apps on|off dışında yönetici yetkisi gerekmez. install/start/run seçeneği: --dns (DNS-over-HTTPS)
Dil: FRAGMA_LANG=tr veya en (varsayılan: Windows görüntü dili)
`,
		"usage.other": `fragma %s (bu sistemde deneysel): SNI tabanlı erişim engellerini aşar (Discord, Roblox, ...)

  fragma proxy            yerel HTTPS proxy'sini başlat, sonra proxy olarak 127.0.0.1:8899 gir
  fragma add <site>       bu siteyi de engelli say
  fragma list             fragma hangi siteleri değiştiriyor? (diğer hepsi: hiçbiri)

  fragma probe            bu bağlantıda her site nasıl engelleniyor?
  fragma experiment       operatörünün DPI'ı hangi tekniği anlamıyor?

Sistem geneli mod (install/stop/start) yalnızca Linux ve Windows'ta var.
Dil: FRAGMA_LANG=tr veya en (varsayılan: sistem dilinden)
`,
		"usage": `fragma %s: SNI tabanlı erişim engellerini aşar (Discord, Roblox, ...)

  sudo fragma install     kur ve başlat (her açılışta da başlar)
  sudo fragma stop        kapat
  sudo fragma start       tekrar aç
  fragma status           açık mı?
  sudo fragma uninstall   her şeyi kaldır

  sudo fragma add <site>  bu siteyi de engelli say
  fragma list             fragma hangi siteleri değiştiriyor? (diğer hepsi: hiçbiri)
  sudo fragma dns on|off  isteğe bağlı: DNS'i DNS-over-HTTPS ile yanıtla (varsayılan kapalı)

  fragma probe            bu bağlantıda her site nasıl engelleniyor?
  fragma experiment       operatörünün DPI'ı hangi tekniği anlamıyor?
  fragma proxy            yerel HTTPS proxy'si, root gerekmez (uygulama başına)
  sudo fragma run         sistem geneli mod, ön planda (servis bunu kullanır)

Dil: FRAGMA_LANG=tr veya en (varsayılan: sistem dilinden)
`,
	},
}
