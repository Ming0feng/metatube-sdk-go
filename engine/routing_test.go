package engine

import "testing"

func TestClassifyNumber(t *testing.T) {
	cases := []struct {
		number string
		want   ContentType
	}{
		{"MIDV-123", ContentTypeCensored},
		{"SSIS-033", ContentTypeCensored},
		{"HEYZO-1234", ContentTypeUncensored},
		{"n0716", ContentTypeUncensored},
		{"k1363", ContentTypeUncensored},
		{"DSAM-13", ContentTypeUncensored},
		{"010115-001", ContentTypeUncensored},
		{"FC2-123456", ContentTypeFC2},
		{"FC2-PPV-123456", ContentTypeFC2},
		{"MD0165", ContentTypeChinese},
		{"MKY-NS-001", ContentTypeChinese},
		{"Blacked.24.01.01", ContentTypeWestern},
		{"968732", ContentTypeHentai},
	}
	for _, c := range cases {
		if got := classifyNumber(c.number); got != c.want {
			t.Errorf("classifyNumber(%q) = %q, want %q", c.number, got, c.want)
		}
	}
}

func TestAllowedProviderNames(t *testing.T) {
	t.Run("censored", func(t *testing.T) {
		allowed := allowedProviderNames("MIDV-123")
		mustHave := []string{"JavDB", "JavBus", "FANZA"}
		mustNotHave := []string{"HEYZO", "FC2", "1Pondo"}
		for _, name := range mustHave {
			if _, ok := allowed[name]; !ok {
				t.Errorf("censored route should include %s", name)
			}
		}
		for _, name := range mustNotHave {
			if _, ok := allowed[name]; ok {
				t.Errorf("censored route should exclude %s", name)
			}
		}
	})

	t.Run("uncensored", func(t *testing.T) {
		allowed := allowedProviderNames("HEYZO-1234")
		mustHave := []string{"JavDB", "HEYZO"}
		mustNotHave := []string{"FANZA", "FC2"}
		for _, name := range mustHave {
			if _, ok := allowed[name]; !ok {
				t.Errorf("uncensored route should include %s", name)
			}
		}
		for _, name := range mustNotHave {
			if _, ok := allowed[name]; ok {
				t.Errorf("uncensored route should exclude %s", name)
			}
		}
	})

	t.Run("fc2", func(t *testing.T) {
		allowed := allowedProviderNames("FC2-123456")
		mustHave := []string{"JavDB", "FC2", "FC2PPVDB"}
		mustNotHave := []string{"FANZA", "HEYZO"}
		for _, name := range mustHave {
			if _, ok := allowed[name]; !ok {
				t.Errorf("fc2 route should include %s", name)
			}
		}
		for _, name := range mustNotHave {
			if _, ok := allowed[name]; ok {
				t.Errorf("fc2 route should exclude %s", name)
			}
		}
	})
}
