# pitwall-drs (Data Relay Service)
This is a basic Go application designed to act as a proxy and fanout for the Formula 1 TV live timing data stream (SignalR).

## Purpose
I want to make a browser based customizable dashboard for live F1 data. This intermediate proxy aleviates a lot of pain with that. 

## Challenge
This api is not ideal for any of the nice json marshalling libraries, so a lot of custom parsing logic is required. As well as live data only coming during F1 events. I do intend to make some mechanism to handle playback of past events for testing. 

#### Very much scuffed
This project is intended for personal or educational use and relies on undocumented APIs. Its continued functionality depends on the stability of the F1TV live timing endpoints and data format, which may change without notice.
