package dostup

import "testing"

func TestClassifyReply(t *testing.T) {
	cases := []struct {
		name string
		text string
		want ReplyKind
	}{
		{
			// живой автоответ ДСА от 27.08.2026 (incoming-1172)
			name: "dsa-live-ack",
			text: "Доброго дня! Ваш лист отримано З повагою, Державна судова адміністрація України",
			want: ReplyAcknowledgement,
		},
		{
			name: "registered-promise",
			text: "Ваше звернення отримано. Зареєстровано за вх. №2/1581 від 28.08.2026. Буде розглянуто у встановлений законом термін.",
			want: ReplyAcknowledgement,
		},
		{
			name: "thanks-ack",
			text: "Дякуємо за звернення! Ваш запит зареєстровано в системі.",
			want: ReplyAcknowledgement,
		},
		{
			name: "substantive-list",
			text: "Відповідно до вашого запиту надаємо перелік електронних адрес судів для звернень громадян.",
			want: ReplySubstantive,
		},
		{
			name: "substantive-rejection",
			text: "У наданні запитуваної інформації відмовлено на підставі п. 3 ч. 2 ст. 6 ЗУ «Про доступ до публічної інформації».",
			want: ReplySubstantive,
		},
		{
			name: "substantive-with-link",
			text: "Ваш лист отримано. Інформацію розміщено за адресою https://dsa.court.gov.ua/addresses",
			want: ReplySubstantive,
		},
		{
			name: "substantive-long",
			text: trimLen('а', 700),
			want: ReplySubstantive,
		},
		{
			name: "ru-ack",
			text: "Ваше обращение получено, будет рассмотрено в установленный срок.",
			want: ReplyAcknowledgement,
		},
		{
			name: "en-auto",
			text: "This is an automatic reply. Thank you for your message.",
			want: ReplyAcknowledgement,
		},
		{
			name: "short-received",
			text: "Лист отримано.",
			want: ReplyAcknowledgement,
		},
		{
			name: "short-mysterious-substantive",
			text: "Так, належить до компетенції ДБР.",
			want: ReplySubstantive,
		},
		{
			name: "empty",
			text: "",
			want: ReplyUnknown,
		},
	}
	for _, tc := range cases {
		if got := ClassifyReply(tc.text); got != tc.want {
			t.Errorf("%s: got %v (%s), want %v\n text: %.120s",
				tc.name, got, KindLabel(got), tc.want, tc.text)
		}
	}
}

func trimLen(r rune, n int) string {
	b := make([]rune, n)
	for i := range b {
		if i%12 == 0 {
			b[i] = ' '
		} else {
			b[i] = r
		}
	}
	return string(b)
}
