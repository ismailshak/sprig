package http

import "net/http"

// contentSecurityPolicy is the Content-Security-Policy header set on every
// response.
//
// sprig serves its own stylesheets, fonts, htmx and scripts and loads nothing
// from another origin, so every fetch directive is 'self'. script-src needs no
// nonce, no 'unsafe-inline' and no 'unsafe-eval'. htmx evaluates code only for
// hx-on, js: values and hx-trigger filters, and no template uses any of them.
//
// style-src-attr 'unsafe-inline' is the one exception. A logged care row is
// rendered with a style attribute setting --grace to the length of that row's
// undo window, and the value differs from row to row, so no hash covers it.
// Style elements stay at 'self'. A browser that does not implement
// style-src-attr, Safari before 15.4 and Firefox before 108, applies style-src
// to the attribute and blocks it. The undo bar then drains over the
// stylesheet's default 4000ms instead of the row's own window.
//
// img-src allows blob: so the plant form can show the photo the browser
// resized before the form posts it. Only the page that created a blob: URL
// can read it.
//
// frame-ancestors 'none' stops another site putting a page in a frame. Logging
// a care, removing a member and revoking a token are each a single button, so
// one click on a framed page could do any of them.
//
// Cloudflare's optional script injections, Rocket Loader and Browser Insights
// among them, would be blocked by this policy and are left off at the edge.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"style-src-attr 'unsafe-inline'; " +
	"img-src 'self' blob:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// permissionsPolicy is the Permissions-Policy header set on every response.
// camera=(self) lets sprig's own pages open the camera for a progress photo.
// The features after it are switched off because the app has no use for them.
// Passkeys go through publickey-credentials-get and -create, whose default is
// already 'self', so neither is named here.
const permissionsPolicy = "camera=(self), microphone=(), geolocation=(), payment=(), usb=()"

// SecurityHeaders sets the four response headers that apply to the whole site
// rather than to any one route. It sets them before calling the next handler,
// so a redirect, a 404 and the 500 Recover writes have them as well as a page.
//
// Strict-Transport-Security is not one of them. Cloudflare terminates TLS and
// reaches the app over plain HTTP, so an HSTS header from here would describe
// a connection the app is not serving. It belongs at the edge.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// An invite URL contains the token that redeems it. same-origin
		// stops the browser sending that URL to another site as a Referer.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Permissions-Policy", permissionsPolicy)
		next.ServeHTTP(w, r)
	})
}
