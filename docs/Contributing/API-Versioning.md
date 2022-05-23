# API versioning

Currently, all Fleet API routes include `v1` in their URL path. 

In the next major version release (5.0.0), Fleet will no longer support `v1`. Instead, all API routes will
support specifying a date (ex. `2022-07`) or `latest` in he place of `v1`. 

In minor version releases following 5.0.0, Fleet may introduce breaking changes to the API. Each release
that includes breaking changes will introduce a new dated version.

When a new dated version is introduced, the previous dated version is supported for the next 6 months.

## Why introduce dated versioning?

The Fleet API is part of the Fleet product. Like the Fleet UI and fleetctl CLI, we'd like to design the best API.

Dated versioning allows for more iteration on the Fleet API while providing users with ample time to
adjust integrations.

## Why include `latest`?

???

## How are breaking changes introduced?

Let's use an example. In `handler.go` we have the following endpoint:

```go
e := NewUserAuthenticatedEndpointer(svc, opts, r, "latest", "2021-11")

// other endpoints here

e.GET("/api/latest/fleet/carves/{id:[0-9]+}/block/{block_id}", getCarveBlockEndpoint, getCarveBlockRequest{})
```

The versions available are `latest` and `2021-11`. This means that the following are valid API paths:

```
/api/latest/fleet/carves/1/block/1234
/api/2021-11/fleet/carves/1/block/1234
```

Now let's say we want to introduce a breaking change. We then specify the version this particular
API is being supported and then add the new one that will only be available starting in the new
version:

```go
e := NewUserAuthenticatedEndpointer(svc, opts, r, "2021-11", "2021-12")

// other endpoints here

e.EndingAtVersion("2021-11").GET("/api/latest/fleet/carves/{id:[0-9]+}/block/{block_id}", getCarveBlockEndpointDeprecated, getCarveBlockRequestDeprecated{})
e.StartingAtVersion("2021-12").GET("/api/latest/fleet/carves/{id:[0-9]+}/block/{block_id}", getCarveBlockEndpoint, getCarveBlockRequest{})
```

This will mean that the following are all valid paths:

```
/api/latest/fleet/carves/1/block/1234
/api/2021-11/fleet/carves/1/block/1234
/api/2021-12/fleet/carves/1/block/1234
```

`/api/2021-12/fleet/carves/1/block/1234` is the new dated version and `/api/2021-11/fleet/carves/1/block/1234` is the previous dated version.

After 6 months, version `2021-11` will be removed:


```go
e := NewUserAuthenticatedEndpointer(svc, opts, r, "2021-12")

// other endpoints here

e.GET("/api/latest/fleet/carves/{id:[0-9]+}/block/{block_id}", getCarveBlockEndpoint, getCarveBlockRequest{})
```

This means that the following are the only valid paths after this point:

```
/api/latest/fleet/carves/1/block/1234
/api/2021-12/fleet/carves/1/block/1234
```

And the code doesn't have to specify `.StartingAtVersion("2021-12")` anymore.

<meta name="pageOrderInSection" value="900">
