package main

// buildFault is empty in every release. Tests set it at build time
// (-ldflags "-X main.buildFault=yes") to get a version that does not start.
var buildFault string
