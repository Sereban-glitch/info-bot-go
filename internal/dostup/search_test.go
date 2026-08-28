package dostup

import "testing"

// Живой фрагмент страницы поиска портала (/search/зарплати/all).
const searchPageFixture = `
<div class="request_listing">
  <p class="request-single">
    <span class="head">
        <a href="/request/propozitsiyi_ighoria_tieriekhova">Пропозиції Ігоря Терехова до Державного бюджету України на 2027 рік</a>
    </span>
  </p>
   <div class="requester">
    Запит відіслано до <a href="https://dostup.org.ua/body/kabiniet_ministriv_ukrayini">Кабінет Міністрів України</a> користувачем <a href="https://dostup.org.ua/user/anna_veklych">Анна Веклич</a> <time datetime="2026-08-26T11:05:59+03:00" title="2026-08-26 11:05:59 +0300">26 Серпня 2026</time>
  </div>
  <p>
    <i class="icon-standalone icon_waiting_response"></i>
    <strong>
      Очікує відповіді
    </strong>
  </p>
</div>
<div class="request_listing">
  <p class="request-single">
    <span class="head">
        <a href="/request/shtatnii_rozpis_minkultu_ta_zarp">Штатний розпис Мінкульту та зарплати керівників</a>
    </span>
  </p>
   <div class="requester">
    Запит відіслано до <a href="https://dostup.org.ua/body/ministerstvo_kulturi_ta_informatiki">Міністерство культури та інформаційної політики</a> користувачем <a href="https://dostup.org.ua/user/oleksii_5">Олексій</a> <time datetime="2026-06-08T10:41:39+03:00">08 Червня 2026</time>
  </div>
  <p>
    <i class="icon-standalone icon_successful"></i>
    <strong>
      Успішний
    </strong>
  </p>
</div>
`

func TestParseSearchResults(t *testing.T) {
	items := parseSearchResults(searchPageFixture)
	if len(items) != 2 {
		t.Fatalf("ожидалось 2 запроса, получено %d", len(items))
	}
	first := items[0]
	if first.Slug != "propozitsiyi_ighoria_tieriekhova" {
		t.Errorf("слаг: %s", first.Slug)
	}
	if first.Title != "Пропозиції Ігоря Терехова до Державного бюджету України на 2027 рік" {
		t.Errorf("тема: %s", first.Title)
	}
	if first.BodyName != "Кабінет Міністрів України" {
		t.Errorf("орган: %s", first.BodyName)
	}
	if first.Date != "2026-08-26" {
		t.Errorf("дата: %s", first.Date)
	}
	if first.Status != "waiting" {
		t.Errorf("статус: %s (ожидался waiting после нормализации)", first.Status)
	}
	if first.URL != BaseURL+"/request/propozitsiyi_ighoria_tieriekhova" {
		t.Errorf("url: %s", first.URL)
	}
	second := items[1]
	if second.Status != "successful" {
		t.Errorf("статус 2: %s", second.Status)
	}
}

func TestSearchURL(t *testing.T) {
	got := SearchURL("зарплати судді")
	want := BaseURL + "/search/%D0%B7%D0%B0%D1%80%D0%BF%D0%BB%D0%B0%D1%82%D0%B8%20%D1%81%D1%83%D0%B4%D0%B4%D1%96/all"
	if got != want {
		t.Errorf("SearchURL:\n got  %s\n want %s", got, want)
	}
}

func TestBodyStatsOverduePct(t *testing.T) {
	st := &BodyStats{Requests: 8, Overdue: 2, Successful: 3}
	if st.OverduePct() != 25 {
		t.Errorf("OverduePct: %d (ожидалось 25)", st.OverduePct())
	}
	empty := &BodyStats{}
	if empty.OverduePct() != 0 {
		t.Errorf("OverduePct пустой: %d", empty.OverduePct())
	}
}
