package httpserver

import "strings"

// allergenLabel names an official allergen marker in both interface languages.
// The model compares these names with the user's own words; the card shows one.
type allergenLabel struct {
	JA string `json:"ja"`
	EN string `json:"en"`
}

// The co-op site marks allergens with CSS classes (icon_egg, ...). These are
// all the classes in https://west2-univ.jp/sp/css/style.css as of 2026-11-23,
// with the Japanese names from its comments. A "_origin" suffix means
// 「〜由来」 (an ingredient derived from it).
var allergenNames = map[string]allergenLabel{
	"egg": {"卵", "egg"}, "milk": {"乳", "milk"}, "wheat": {"小麦", "wheat"},
	"buckwheat": {"そば", "buckwheat"}, "peanut": {"落花生", "peanut"},
	"shrimp": {"えび", "shrimp"}, "crab": {"かに", "crab"}, "walnut": {"くるみ", "walnut"},
	"almond": {"アーモンド", "almond"}, "abalone": {"あわび", "abalone"}, "squid": {"いか", "squid"},
	"salmon_roe": {"いくら", "salmon roe"}, "orange": {"オレンジ", "orange"},
	"cashew_nuts": {"カシューナッツ", "cashew nut"}, "kiwi_fruit": {"キウイフルーツ", "kiwi fruit"},
	"beef": {"牛肉", "beef"}, "sesame": {"ごま", "sesame"}, "salmon": {"さけ", "salmon"},
	"mackerel": {"さば", "mackerel"}, "soybean": {"大豆", "soybean"}, "chicken": {"鶏肉", "chicken"},
	"banana": {"バナナ", "banana"}, "pork": {"豚肉", "pork"}, "matsutake_mushroom": {"まつたけ", "matsutake mushroom"},
	"peach": {"もも", "peach"}, "yam": {"やまいも", "yam"}, "apple": {"りんご", "apple"},
	"gelatin": {"ゼラチン", "gelatin"}, "macadamianuts": {"マカダミアナッツ", "macadamia nut"},
	"seafood": {"魚介類", "seafood"},
}

// allergenLabels keeps nil (not published or unreadable) apart from an empty list.
// A marker added to the site later is passed on under its code rather than dropped.
func allergenLabels(codes []string, valid bool) []allergenLabel {
	if !valid {
		return nil
	}
	out := make([]allergenLabel, 0, len(codes))
	for _, code := range codes {
		base, derived := strings.CutSuffix(code, "_origin")
		label, ok := allergenNames[base]
		if !ok {
			label = allergenLabel{code, code}
		} else if derived {
			label = allergenLabel{label.JA + "由来", "derived from " + label.EN}
		}
		out = append(out, label)
	}
	return out
}
