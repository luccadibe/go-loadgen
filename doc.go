/*
Package go_loadgen provides a protocol-agnostic, open-loop load generator.

Applications provide typed Client, DataProvider, and Collector implementations.
NewEndpoint adapts them into a heterogeneous endpoint set, while NewWorkload
validates phases and compiles weighted target routing before a run begins.

Run stops scheduling at phase boundaries and drains issued requests by default.
An optional drain timeout cancels requests that remain after scheduling ends.
*/
package go_loadgen
