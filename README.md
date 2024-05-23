# Prover market page
WIP implementation of the prover market page where anyone can permissionlessly add a prover. Before creation the `onRecordBeforeCreateRequest` hook checks if the endpoint is a valid prover. 

Built with [pocketbase](https://pocketbase.io)

## Caching

The app uses Redis as a cache, with the following logic:

- no data cached 
  --> manually go through the endpoints, serve + cache the result
- data is cached and is <1 hour old 
  --> serve the cached data
- data is cached but is >1 hour old 
  --> serve the 'stale' cached data, but start a background process to renew the cached data
- data is cached and is >24 hours old 
  --> the data is automatically deleted by Redis, so the next request will have to wait some time before getting results. This is a good measure to prevent overloading endpoints

## Backups

The data lives inside the `/pb_data` folder and can be backed up/imported by copying the file. Make sure to shut down the docker compose before doing any actions though.
