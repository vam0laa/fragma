package main

import (
	"fmt"
	"os"
	"strings"
)

// lang is "tr" or "en". Chosen from FRAGMA_LANG, then the usual locale
// variables (LC_ALL, LC_MESSAGES, LANG); anything that is not Turkish is English.
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
	return "en"
}

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
		"fail.root":      "this needs root: sudo fragma %s",
		"fail.copy":      "installing the binary: %v",
		"fail.unit":      "writing the service file: %v",
		"fail.reload":    "systemctl daemon-reload failed (is this a systemd system?)",
		"fail.enable":    "could not enable the service, see: journalctl -u fragma",
		"fail.start":     "could not start the service, see: journalctl -u fragma",
		"fail.stayup":    "the service did not stay up, see: journalctl -u fragma -n 30",
		"fail.ctl":       "systemctl %s fragma failed (installed? run the install command first)",
		"fail.addusage":  "usage: sudo fragma add example.com [more.org ...]",
		"fail.badhost":   "not a host name: %q",
		"fail.dnsusage":  "usage: sudo fragma dns on [--doh URL] | off | status",
		"fail.restart":   "could not restart the service, see: journalctl -u fragma",
		"step.installed": "Installed",
		"step.service":   "Service",
		"step.running":   "fragma",
		"step.removed":   "Removed",
		"step.known":     "Known",
		"step.added":     "Added",
		"step.dns":       "DNS",
		"d.enabled":      "enabled, starts at every boot",
		"d.running":      "running",
		"d.removed":      "service, rules and program are gone",
		"d.applies":      "applies within 10 seconds",
		"d.dnson":        "answered over DNS-over-HTTPS; your lookups go to that provider, not to your ISP",
		"d.dnsoff":       "untouched again",
		"word.on":        "ON",
		"word.off":       "OFF",
		"box.title":      "fragma is ON",
		"box.l1":         "Discord, Roblox and other blocked sites",
		"box.l2":         "should open now. Every other site is",
		"box.l3":         "left exactly as it is.",
		"box.stop":       "turn it off",
		"box.start":      "turn it on again",
		"box.status":     "is it on?",
		"box.add":        "add a blocked site",
		"box.uninstall":  "remove everything",
		"ctl.on":         "fragma is %s.",
		"ctl.off":        "fragma is %s. Nothing is redirected anymore.",
		"st.on":          "fragma is %s  %s",
		"st.off":         "fragma is %s  %s",
		"st.onhint":      "(stop with: sudo fragma stop)",
		"st.offhint":     "(start with: sudo fragma start)",
		"list.builtin":   "Built in",
		"list.always":    "always",
		"list.yours":     "Yours",
		"list.learned":   "Learned",
		"list.found":     "found blocked by fragma itself",
		"list.none":      "(none)",
		"list.footer":    "Every other site is passed through unchanged.",
		"dns.on":         "DNS-over-HTTPS is %s",
		"dns.off":        "DNS is %s (your system DNS is used as is)",
		"dns.untouched":  "untouched",
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
		"fail.root":      "bunun için yönetici yetkisi gerekir: sudo fragma %s",
		"fail.copy":      "program kurulamadı: %v",
		"fail.unit":      "servis dosyası yazılamadı: %v",
		"fail.reload":    "systemctl daemon-reload başarısız (sistem systemd tabanlı mı?)",
		"fail.enable":    "servis etkinleştirilemedi, bak: journalctl -u fragma",
		"fail.start":     "servis başlatılamadı, bak: journalctl -u fragma",
		"fail.stayup":    "servis ayakta kalmadı, bak: journalctl -u fragma -n 30",
		"fail.ctl":       "systemctl %s fragma başarısız (kurulu mu? önce kurulum komutunu çalıştır)",
		"fail.addusage":  "kullanım: sudo fragma add example.com [diger.org ...]",
		"fail.badhost":   "geçerli bir site adı değil: %q",
		"fail.dnsusage":  "kullanım: sudo fragma dns on [--doh URL] | off | status",
		"fail.restart":   "servis yeniden başlatılamadı, bak: journalctl -u fragma",
		"step.installed": "Kuruldu",
		"step.service":   "Servis",
		"step.running":   "fragma",
		"step.removed":   "Kaldırıldı",
		"step.known":     "Biliniyor",
		"step.added":     "Eklendi",
		"step.dns":       "DNS",
		"d.enabled":      "etkin, her açılışta başlar",
		"d.running":      "çalışıyor",
		"d.removed":      "servis, kurallar ve program kaldırıldı",
		"d.applies":      "10 saniye içinde geçerli olur",
		"d.dnson":        "DNS-over-HTTPS ile yanıtlanıyor; aramaların operatörüne değil o sağlayıcıya gider",
		"d.dnsoff":       "yeniden dokunulmuyor",
		"word.on":        "AÇIK",
		"word.off":       "KAPALI",
		"box.title":      "fragma AÇIK",
		"box.l1":         "Discord, Roblox ve engelli diğer siteler",
		"box.l2":         "şimdi açılmalı. Diğer tüm siteler",
		"box.l3":         "olduğu gibi bırakılır.",
		"box.stop":       "kapat",
		"box.start":      "tekrar aç",
		"box.status":     "açık mı?",
		"box.add":        "engelli site ekle",
		"box.uninstall":  "her şeyi kaldır",
		"ctl.on":         "fragma %s.",
		"ctl.off":        "fragma %s. Artık hiçbir şey yönlendirilmiyor.",
		"st.on":          "fragma %s  %s",
		"st.off":         "fragma %s  %s",
		"st.onhint":      "(durdurmak için: sudo fragma stop)",
		"st.offhint":     "(başlatmak için: sudo fragma start)",
		"list.builtin":   "Yerleşik",
		"list.always":    "her zaman",
		"list.yours":     "Senin",
		"list.learned":   "Öğrenilen",
		"list.found":     "fragma'nın kendi bulduğu engelli siteler",
		"list.none":      "(yok)",
		"list.footer":    "Diğer tüm siteler olduğu gibi geçer.",
		"dns.on":         "DNS-over-HTTPS %s",
		"dns.off":        "DNS %s (sistem DNS'in olduğu gibi kullanılıyor)",
		"dns.untouched":  "dokunulmuyor",
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
