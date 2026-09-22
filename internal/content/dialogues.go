package content

type Dialogue struct {
	ID, Character string
	Unlock        int
	Text          string
}

var Dialogues = []Dialogue{
	{"wolf-hello", "wolf_character", 0, "Не стой в дверях. Я уже учуял неприятности."},
	{"wolf-hunt", "wolf_character", 250, "Стая — это те, кто не бросает бой на половине."},
	{"wolf-scar", "wolf_character", 750, "Шрамы не украшают. Они напоминают, где ты ошибся."},
	{"wolf-trust", "wolf_character", 1500, "Ладно. Тебе я спину доверю. Только не привыкай."},
	{"singer-hello", "singer_character", 0, "Публика пришла? Тогда не моргай на припеве."},
	{"singer-stage", "singer_character", 250, "Сцена любит смелых. И тех, кто попадает в ноты."},
	{"singer-silence", "singer_character", 750, "Иногда пауза звучит громче любого крика."},
	{"singer-duet", "singer_character", 1500, "Дуэт? Хорошо. Но последний аккорд всё равно мой."},
	{"jelly-hello", "jellyfish_character", 0, "Не трогай щупальца. Они трогают в ответ."},
	{"jelly-current", "jellyfish_character", 250, "Течение не спорит со стеной. Оно находит щель."},
	{"jelly-depth", "jellyfish_character", 750, "На глубине тихо. Поэтому там слышно правду."},
	{"jelly-friend", "jellyfish_character", 1500, "Ты странный сухопутный. Кажется, мне нравится."},
	{"angel-hello", "angel_wind_character", 0, "Ветер сегодня добрый. Я — не обещаю."},
	{"angel-flight", "angel_wind_character", 250, "Падение начинается, когда перестаёшь смотреть вверх."},
	{"angel-storm", "angel_wind_character", 750, "Буря не злится. Она просто ничего не сдерживает."},
	{"angel-secret", "angel_wind_character", 1500, "Если услышишь моё настоящее имя — забудь его."},
	{"clown-hello", "clown_character", 0, "Улыбнись! Самое страшное ещё даже не смешное."},
	{"clown-joke", "clown_character", 250, "Хорошая шутка заканчивается смехом. Отличная — сиреной."},
	{"clown-mask", "clown_character", 750, "Под маской ещё одна маска. Экономлю на лицах."},
	{"clown-finale", "clown_character", 1500, "Ты досмотрел до финала. Значит, мы оба ненормальные."},
	{"botanist-hello", "botanist_character", 0, "Не наступай на ростки. Они запоминают лица."},
	{"botanist-roots", "botanist_character", 250, "Корни работают молча, пока остальные хвастаются цветами."},
	{"botanist-thorns", "botanist_character", 750, "Шипы — это не агрессия. Это чёткие границы."},
	{"botanist-bloom", "botanist_character", 1500, "Редкий цветок распускается только рядом с тем, кому верит."},
	{"horned-hello", "horned_girl_character", 0, "Рога настоящие. Вопросы — тоже будут больно."},
	{"horned-charge", "horned_girl_character", 250, "Если дверь закрыта, разгон просто должен быть длиннее."},
	{"horned-pride", "horned_girl_character", 750, "Я не упрямая. Мир просто слишком медленно соглашается."},
	{"horned-oath", "horned_girl_character", 1500, "Скажешь, куда бежать — я пробью дорогу. Один раз."},
}

func DialoguesFor(name string) []Dialogue {
	var out []Dialogue
	for _, d := range Dialogues {
		if d.Character == name {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		out = append(out, Dialogue{name + "-hello", name, 0, "Ну что, сыграемся? Покажи, на что мы способны."})
	}
	return out
}
