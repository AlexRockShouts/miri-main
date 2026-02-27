# Config


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**storage_dir** | **string** |  | [optional] [default to undefined]
**server** | [**ConfigServer**](ConfigServer.md) |  | [optional] [default to undefined]
**models** | [**ConfigModels**](ConfigModels.md) |  | [optional] [default to undefined]
**agents** | [**ConfigAgents**](ConfigAgents.md) |  | [optional] [default to undefined]
**channels** | [**ConfigChannels**](ConfigChannels.md) |  | [optional] [default to undefined]

## Example

```typescript
import { Config } from './api';

const instance: Config = {
    storage_dir,
    server,
    models,
    agents,
    channels,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
