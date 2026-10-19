package javdb

import (
	"testing"

	"github.com/metatube-community/metatube-sdk-go/provider/internal/testkit"
)

func TestJavDB_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"yx3y4d",
	})
}

func TestJavDB_SearchMovie(t *testing.T) {
	testkit.Test(t, New, []string{
		"MIDA-823",
		"SSIS-033",
		"DSAM-13",
		"DSAM-013",
	})
}
