import unittest
from bs4 import BeautifulSoup
from sources import details, listed_items


def soup(html):
    return BeautifulSoup(html, "html.parser")


def detail_page(kcal="553", protein="18.9", allergy='<ul class="icon-list"><li class="icon_egg"></li></ul>'):
    rows = "".join(f'<li><strong>{label}</strong><span class="price">{value}</span></li>'
                   for label, value in [("エネルギー", kcal+"kcal"), ("タンパク質", protein+"g"),
                                        ("脂質","36.9g"), ("炭水化物","36.6g"), ("食塩相当量","4.0g")])
    return soup(f'<div id="main"><h1>チキン南蛮<span>Chicken</span></h1><ul class="detail">{rows}<li class="allergy">{allergy}</li></ul></div>')


class SourceTests(unittest.TestCase):
    def test_details_missing_is_different_from_zero(self):
        self.assertIsNone(details(detail_page("-"), "チキン南蛮"))
        actual = details(detail_page("0", "-"), "チキン南蛮")
        self.assertEqual(actual["calories"], 0)
        self.assertIsNone(actual["protein_g"])
        self.assertEqual(actual["allergens"], ["egg"])
        self.assertEqual(actual["extra_nutrients"]["食塩相当量"], {"value":4.0, "unit":"g"})

    def test_live_source_allows_space_before_units(self):
        info = details(detail_page("173 ", "13.5 "), "チキン南蛮")
        self.assertEqual((info["calories"], info["protein_g"]), (173, 13.5))

    def test_unfamiliar_details_are_retryable_and_missing_allergy_is_unknown(self):
        for page in [detail_page("broken"), soup("<h1>Error</h1>"), detail_page()]:
            with self.assertRaises(ValueError):
                details(page, "Wrong dish")
        with self.assertRaises(ValueError):
            details(detail_page("broken"), "チキン南蛮")
        self.assertIsNone(details(detail_page(allergy=''), "チキン南蛮")["allergens"])
        self.assertIsNone(details(detail_page(allergy='<ul class="icon-list"><li class="new"></li></ul>'), "チキン南蛮")["allergens"])

    def test_every_category_kept_and_store_validated(self):
        row = {"name":"朝定食","name_en":None,"member_price_yen":385,"status":"unmarked",
               "url":"https://west2-univ.jp/sp/detail.php?t=650111&c=650118_100"}
        result = listed_items([{"category":cat,"items":[row]} for cat in ["和食","朝食プレート"]], "650111")
        self.assertEqual(result[0]["categories"], ["和食", "朝食プレート"])
        self.assertEqual(result[0]["dish_code"], "650118_100")
        with self.assertRaises(ValueError):
            listed_items([{"category":"x","items":[row]}], "650112")
