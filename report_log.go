package main

import "strings"

// logEvent is a message fragma knows, with how often it appeared. Nothing of the
// line itself is kept.
type logEvent struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// Messages from the service, the system and the Windows error log that are not
// translated.
var logPatterns = []struct{ code, text string }{
	{"started", "fragma is on"},
	{"stopped", "fragma is off: rules removed"},
	{"needs_root", "system-wide mode needs root"},
	{"rules_failed", "is nftables installed"},
	{"rules_failed", "nft -"},
	{"doh_lookup_failed", "cannot look up "},
	{"doh_url_invalid", "not a DNS-over-HTTPS URL"},
	{"port_in_use", "address already in use"},
	{"port_in_use", "Only one usage of each socket address"},
	{"app_relay_failed", "app relay:"},
	{"guard_failed", "guard:"},
	{"crashed", "panic:"},
	{"crashed", "fatal error:"},
	{"service_failed", "Failed with result"},
	{"restart_limit", "Start request repeated too quickly"},
	{"restarted", "Scheduled restart job"},
	{"service_started", "Started fragma"},
	{"service_stopped", "Stopped fragma"},
	{"service_stopping", "Stopping fragma"},
	{"service_stopped_cleanly", "Deactivated successfully"},
	// the clean-up after a stop finds the rules already gone
	{"nft_rule_message", "Could not process rule"},
}

// Translated messages are recognised by the fixed text before their first value.
var logMessages = map[string]string{
	"win.restorefail": "restore_failed",
	"win.statelost":   "state_damaged",
	"win.statebad":    "state_damaged",
	"win.appsleft":    "hosts_entries_left",
	"win.already":     "already_running",
	"win.foreign":     "other_proxy_in_use",
	"win.updfailed":   "update_failed",
}

func logCode(line string) string {
	for key, code := range logMessages {
		for _, m := range catalog {
			if prefix, _, _ := strings.Cut(m[key], "%"); prefix != "" && strings.Contains(line, prefix) {
				return code
			}
		}
	}
	for _, p := range logPatterns {
		if strings.Contains(line, p.text) {
			return p.code
		}
	}
	return ""
}

// classifyLog counts the known messages in lines, in the order they first appear,
// and the lines that are none of them.
func classifyLog(lines []string) ([]logEvent, int) {
	events, unknown := []logEvent{}, 0
	at := map[string]int{}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		code := logCode(l)
		if code == "" {
			unknown++
			continue
		}
		i, ok := at[code]
		if !ok {
			i = len(events)
			at[code] = i
			events = append(events, logEvent{Code: code})
		}
		events[i].Count++
	}
	return events, unknown
}

func init() {
	for k, v := range map[string][2]string{
		"rep.testing":      {"testing a few sites...", "birkaç site deneniyor..."},
		"rep.nothing":      {"Nothing was sent. Read the report; if you want to share it, paste it here:", "Hiçbir şey gönderilmedi. Raporu oku; paylaşmak istersen şuraya yapıştır:"},
		"rep.saved":        {"Saved to %s, exactly as shown above.", "%s dosyasına, yukarıda gösterildiği gibi kaydedildi."},
		"rep.exists":       {"%s already exists; choose another name (nothing was written)", "%s zaten var; başka bir ad seç (hiçbir şey yazılmadı)"},
		"rep.usage":        {"usage: fragma report [--json] [--output FILE] [--no-net]", "kullanım: fragma report [--json] [--output DOSYA] [--no-net]"},
		"rep.form":         {"form", "biçim"},
		"rep.version":      {"version", "sürüm"},
		"rep.date":         {"date", "tarih"},
		"rep.system":       {"system", "sistem"},
		"rep.installed":    {"installed", "kurulu"},
		"rep.running":      {"running", "çalışıyor"},
		"rep.instver":      {"installed version", "kurulu sürüm"},
		"rep.doh":          {"DNS over HTTPS", "HTTPS üzerinden DNS"},
		"rep.own":          {"own sites (how many)", "kendi siteleri (sayı)"},
		"rep.learned":      {"learned sites (how many)", "öğrenilen siteler (sayı)"},
		"rep.tools":        {"other bypass tools", "diğer engel aşma araçları"},
		"rep.ipv6":         {"IPv6", "IPv6"},
		"rep.tunnel":       {"VPN-like connection", "VPN benzeri bağlantı"},
		"rep.kernel":       {"kernel", "çekirdek"},
		"rep.nft":          {"nftables", "nftables"},
		"rep.boot":         {"starts by itself", "kendiliğinden başlar"},
		"rep.proxy":        {"system proxy", "sistem proxy'si"},
		"rep.hosts":        {"hosts file entries (apps)", "hosts dosyası kayıtları (apps)"},
		"rep.pending":      {"settings waiting to be restored", "geri yüklenmeyi bekleyen ayar"},
		"rep.checks":       {"checks", "denetimler"},
		"rep.sites":        {"sites (answers out of tries)", "siteler (denemelerden yanıt alınanlar)"},
		"rep.nottested":    {"not tested", "denenmedi"},
		"rep.lookup":       {"name", "ad"},
		"rep.fragmented":   {"fragmented", "parçalı"},
		"rep.log":          {"known messages in the log", "günlükteki bilinen iletiler"},
		"rep.unrecognised": {"other lines, not included", "diğer satırlar (rapora alınmadı)"},
		"rep.yes":          {"yes", "evet"},
		"rep.no":           {"no", "hayır"},
		"rep.unknown":      {"unknown", "bilinmiyor"},
		"rep.none":         {"none seen", "görülmedi"},
		"box.report":       {"report a problem", "sorun bildir"},
	} {
		catalog["en"][k], catalog["tr"][k] = v[0], v[1]
	}
}
