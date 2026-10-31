//go:build linux

package main

// herd draws its own titlebar. Wails' Frameless option makes the GTK window
// undecorated, and GTK 3 then asks Wayland compositors that speak the KDE
// server-decoration protocol (KDE, COSMIC) to draw *server-side*
// decorations: a native titlebar appears above herd's own bar anyway.
//
// GTK picks client- or server-side decorations once, when the window is
// realized, and Wails realizes it early. So the change is made as the window
// is built: when Wails adds its content box to the new toplevel, herd gives
// the window an empty, zero-height custom titlebar and keeps it decorated
// (overriding Wails' later SetDecorated(false)). GTK then uses client-side
// decorations, the compositor draws none, and the window keeps GTK's shadow
// and resize borders.

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>

static const char *herd_titlebar_css =
	"window.herd-csd .titlebar, window.herd-csd .titlebar:backdrop {"
	"  min-height: 0; padding: 0; margin: 0; border: 0;"
	"  background: none; box-shadow: none;"
	"}";

static void herd_keep_decorated(GObject *obj, GParamSpec *pspec, gpointer data) {
	GtkWindow *w = GTK_WINDOW(obj);
	if (!gtk_window_get_decorated(w)) gtk_window_set_decorated(w, TRUE);
}

// Emission hook for GtkContainer::add: runs when Wails packs its vbox into
// the main window, before the window is realized.
static gboolean herd_on_add(GSignalInvocationHint *hint, guint n, const GValue *params, gpointer data) {
	GObject *obj = g_value_get_object(&params[0]);
	if (!GTK_IS_WINDOW(obj) || n < 2) return TRUE;
	GtkWindow *w = GTK_WINDOW(obj);
	if (gtk_window_get_window_type(w) != GTK_WINDOW_TOPLEVEL) return TRUE;
	if (!GTK_IS_BOX(g_value_get_object(&params[1]))) return TRUE;
	if (g_object_get_data(obj, "herd-csd") || gtk_widget_get_realized(GTK_WIDGET(w))) return TRUE;
	g_object_set_data(obj, "herd-csd", GINT_TO_POINTER(1));

	GtkWidget *bar = gtk_event_box_new();
	gtk_widget_show(bar);
	gtk_window_set_titlebar(w, bar);
	gtk_style_context_add_class(gtk_widget_get_style_context(GTK_WIDGET(w)), "herd-csd");
	g_signal_connect(obj, "notify::decorated", G_CALLBACK(herd_keep_decorated), NULL);
	return FALSE; // one window: remove the hook
}

static int herd_install_titlebar_hook(void) {
	if (!gtk_init_check(NULL, NULL)) return 0;
	GtkCssProvider *css = gtk_css_provider_new();
	gtk_css_provider_load_from_data(css, herd_titlebar_css, -1, NULL);
	gtk_style_context_add_provider_for_screen(gdk_screen_get_default(), GTK_STYLE_PROVIDER(css),
		GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
	g_object_unref(css);
	// Signals exist once their class is initialised; nothing has created a
	// window yet at this point.
	g_type_class_ref(GTK_TYPE_WINDOW);
	g_signal_add_emission_hook(g_signal_lookup("add", GTK_TYPE_CONTAINER), 0, herd_on_add, NULL, NULL);
	return 1;
}
*/
import "C"

import (
	"log"
	"os"
	"runtime"
)

// GTK must be initialised on the main thread, which is the thread main()
// starts on only if it is locked here.
func init() { runtime.LockOSThread() }

// useClientSideTitlebar must run in main() before wails.Run.
func useClientSideTitlebar() {
	// Same backend choice Wails makes in its own (later, now no-op) gtk_init.
	if os.Getenv("GDK_BACKEND") == "" {
		switch os.Getenv("XDG_SESSION_TYPE") {
		case "", "unspecified", "x11":
			_ = os.Setenv("GDK_BACKEND", "x11")
		}
	}
	if C.herd_install_titlebar_hook() == 0 {
		log.Print("herd: GTK init failed; the window may show a native titlebar")
	}
}
