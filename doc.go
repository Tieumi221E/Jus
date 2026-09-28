// Package jus is the shared base of the Jus apps (Jusplay, Jusnote): what
// every one of them does the same way.
//
//   - capreg: the capability registry. Each app registers what it can do
//     once; its command line, `help -json`, the agent guide and the
//     window's API are made from it.
//   - instance: the command line finds the app's running window and hands
//     calls to it.
//   - textenc: text in whatever encoding it was saved in.
//   - apps: an app's manifest and footprint, finding the other apps and
//     calling their capabilities, the uninstall plan.
//   - conform: the conformance test every app passes.
//   - skills: a folder's own tools, run once the user confirmed them.
//   - procstat: a process's own peak memory, for benchmarks.
//
// cmd/jus is the series' small program: jus apps, open, uninstall, conform.
// Nothing app-specific belongs here.
package jus
