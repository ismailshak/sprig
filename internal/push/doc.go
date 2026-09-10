// Package push manages web push subscriptions, sends VAPID-signed
// notifications, and runs the two jobs that send on a timer: the daily digest
// and the deadlines for tokens and sittings.
package push
