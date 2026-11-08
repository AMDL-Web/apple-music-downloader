// Package region names Apple Music storefronts, the lower-case ISO 3166
// codes the backend reports an account's region in, in English, the app's
// language.
package region

import (
	"strings"
	"sync"
)

// names are the storefronts Apple Music serves, as code:name pairs.
const names = "ae:United Arab Emirates|ag:Antigua and Barbuda|ai:Anguilla|al:Albania|am:Armenia|ao:Angola|ar:Argentina|" +
	"at:Austria|au:Australia|az:Azerbaijan|ba:Bosnia and Herzegovina|bb:Barbados|be:Belgium|bf:Burkina Faso|bg:Bulgaria|" +
	"bh:Bahrain|bj:Benin|bm:Bermuda|bn:Brunei|bo:Bolivia|br:Brazil|bs:Bahamas|bt:Bhutan|bw:Botswana|by:Belarus|bz:Belize|" +
	"ca:Canada|cd:Congo (DRC)|cg:Congo|ch:Switzerland|ci:Côte d’Ivoire|cl:Chile|cm:Cameroon|cn:China|co:Colombia|" +
	"cr:Costa Rica|cv:Cape Verde|cy:Cyprus|cz:Czechia|de:Germany|dk:Denmark|dm:Dominica|do:Dominican Republic|dz:Algeria|" +
	"ec:Ecuador|ee:Estonia|eg:Egypt|es:Spain|fi:Finland|fj:Fiji|fm:Micronesia|fr:France|ga:Gabon|gb:United Kingdom|" +
	"gd:Grenada|ge:Georgia|gh:Ghana|gm:Gambia|gr:Greece|gt:Guatemala|gw:Guinea-Bissau|gy:Guyana|hk:Hong Kong|hn:Honduras|" +
	"hr:Croatia|hu:Hungary|id:Indonesia|ie:Ireland|il:Israel|in:India|iq:Iraq|is:Iceland|it:Italy|jm:Jamaica|jo:Jordan|" +
	"jp:Japan|ke:Kenya|kg:Kyrgyzstan|kh:Cambodia|kn:St Kitts and Nevis|kr:South Korea|kw:Kuwait|ky:Cayman Islands|" +
	"kz:Kazakhstan|la:Laos|lb:Lebanon|lc:St Lucia|lk:Sri Lanka|lr:Liberia|lt:Lithuania|lu:Luxembourg|lv:Latvia|ly:Libya|" +
	"ma:Morocco|md:Moldova|me:Montenegro|mg:Madagascar|mk:North Macedonia|ml:Mali|mm:Myanmar|mn:Mongolia|mo:Macao|" +
	"mr:Mauritania|ms:Montserrat|mt:Malta|mu:Mauritius|mv:Maldives|mw:Malawi|mx:Mexico|my:Malaysia|mz:Mozambique|" +
	"na:Namibia|ne:Niger|ng:Nigeria|ni:Nicaragua|nl:Netherlands|no:Norway|np:Nepal|nr:Nauru|nz:New Zealand|om:Oman|" +
	"pa:Panama|pe:Peru|pg:Papua New Guinea|ph:Philippines|pk:Pakistan|pl:Poland|pt:Portugal|pw:Palau|py:Paraguay|qa:Qatar|" +
	"ro:Romania|rs:Serbia|ru:Russia|rw:Rwanda|sa:Saudi Arabia|sb:Solomon Islands|sc:Seychelles|se:Sweden|sg:Singapore|" +
	"si:Slovenia|sk:Slovakia|sl:Sierra Leone|sn:Senegal|sr:Suriname|st:São Tomé and Príncipe|sv:El Salvador|sz:Eswatini|" +
	"tc:Turks and Caicos Islands|td:Chad|th:Thailand|tj:Tajikistan|tm:Turkmenistan|tn:Tunisia|to:Tonga|tr:Türkiye|" +
	"tt:Trinidad and Tobago|tw:Taiwan|tz:Tanzania|ua:Ukraine|ug:Uganda|us:United States|uy:Uruguay|uz:Uzbekistan|" +
	"vc:St Vincent and the Grenadines|ve:Venezuela|vg:British Virgin Islands|vn:Vietnam|vu:Vanuatu|xk:Kosovo|ye:Yemen|" +
	"za:South Africa|zm:Zambia|zw:Zimbabwe"

var (
	once  sync.Once
	table map[string]string
)

// Name is the region code names, "Japan" for jp, or the code in capitals
// for one it does not know; "" for "".
func Name(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	once.Do(func() {
		table = map[string]string{}
		for _, pair := range strings.Split(names, "|") {
			k, v, _ := strings.Cut(pair, ":")
			table[k] = v
		}
	})
	if n, ok := table[code]; ok {
		return n
	}
	return strings.ToUpper(code)
}

// Label names an account by its Apple ID and region: "a@icloud.com ·
// Japan", either alone when the other is "", or fallback when both are.
func Label(name, code, fallback string) string {
	r := Name(code)
	switch {
	case name != "" && r != "":
		return name + " · " + r
	case name != "":
		return name
	case r != "":
		return r
	}
	return fallback
}
