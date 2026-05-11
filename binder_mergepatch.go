package wshell

import (
	"mime"

	"github.com/labstack/echo/v4"
)

type MergePatchBinder struct {
	echo.Binder
}

func (b *MergePatchBinder) Bind(i any, c echo.Context) error {
	ct := c.Request().Header.Get(echo.HeaderContentType)
	mt, _, err := mime.ParseMediaType(ct)
	if err == nil && mt == "application/merge-patch+json" {
		// Tell Echo to treat this as JSON
		c.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	return b.Binder.Bind(i, c)
}
