Main backend for the F1 live data analysis site: https://pitwall.me/

# pitwall-drs (Data Relay Service)
This is designed to act as a proxy and fanout for the Formula 1 TV live timing data stream (SignalR).

## Purpose
I want to make a browser based customizable dashboard for live F1 data. This intermediate proxy aleviates a lot of pain with that. 

## Features
- Allow one connection to the race telemetry websocket to fan out to many thousands of clients
- Full state tracking of global state allowing for easy snapshotting
- Event stream recording
- Replaying of recorded streams (replay.go for now)
- Driver Lap History tracking events and state inserted on top of the current stream of events
- Season schedule loading
- Auto connect/disconnect mode around scheduled sections

## Challenge
This api is not ideal for any of the nice json marshalling libraries, so a lot of custom parsing logic is required. As well as live data only coming during F1 events. I do intend to make some mechanism to handle playback of past events for testing. 

#### Very much scuffed
This project is intended for personal or educational use and relies on undocumented APIs. Its continued functionality depends on the stability of the F1TV live timing endpoints and data format, which may change without notice.
