package store

import "testing"

func TestPlant_OtherNameIsTheNameDisplayNameDidNotUse(t *testing.T) {
	name := func(s string) *string { return &s }

	tests := []struct {
		name      string
		plant     Plant
		want      string
		botanical bool
	}{
		{
			name:  "the common name follows a nickname",
			plant: Plant{Nickname: name("Big Fella"), CommonName: name("Swiss cheese plant"), BotanicalName: name("Monstera deliciosa")},
			want:  "Swiss cheese plant",
		},
		{
			name:      "the botanical name follows a nickname where there is no common one",
			plant:     Plant{Nickname: name("Spike"), BotanicalName: name("Echinocactus grusonii")},
			want:      "Echinocactus grusonii",
			botanical: true,
		},
		{
			name:      "the botanical name follows a common name used as the display name",
			plant:     Plant{CommonName: name("Sweet basil"), BotanicalName: name("Ocimum basilicum")},
			want:      "Ocimum basilicum",
			botanical: true,
		},
		{
			name:  "a plant with one name has no other name",
			plant: Plant{BotanicalName: name("Opuntia microdasys")},
		},
		{
			name:  "an empty string counts as no name",
			plant: Plant{Nickname: name("Sprout"), CommonName: name(""), BotanicalName: name("")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, botanical := tt.plant.OtherName()
			if got != tt.want || botanical != tt.botanical {
				t.Errorf("OtherName() = %q, %v, want %q, %v", got, botanical, tt.want, tt.botanical)
			}
		})
	}
}
