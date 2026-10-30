package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
)

// RunProfiling controls whether each MP3 export writes Go profiles.
const RunProfiling = true

// startExportProfiling captures the whole export, including model loading and
// encoding. The returned function is safe to call once the export finishes or
// fails; the application's control lock serializes export requests.
func startExportProfiling() func() {
	executable, err := os.Executable()
	if err != nil {
		log.Printf("export profiling: executable path: %v", err)
		return func() {}
	}
	dir := filepath.Dir(executable)
	cpuPath := filepath.Join(dir, "go-speak-cpu.pprof")
	heapPath := filepath.Join(dir, "go-speak-heap.pprof")
	file, err := os.Create(cpuPath)
	if err != nil {
		log.Printf("export profiling: create CPU profile: %v", err)
		return func() {}
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		log.Printf("export profiling: start CPU profile: %v", err)
		return func() {}
	}
	log.Printf("export profiling: started CPU profile %s", cpuPath)
	var once sync.Once
	return func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			if err := file.Close(); err != nil {
				log.Printf("export profiling: close CPU profile: %v", err)
			}
			heap, err := os.Create(heapPath)
			if err != nil {
				log.Printf("export profiling: create heap profile: %v", err)
				return
			}
			runtime.GC()
			if err := pprof.WriteHeapProfile(heap); err != nil {
				log.Printf("export profiling: write heap profile: %v", err)
			}
			if err := heap.Close(); err != nil {
				log.Printf("export profiling: close heap profile: %v", err)
			}
			log.Printf("export profiling: profiles saved to %s and %s", cpuPath, heapPath)
		})
	}
}
