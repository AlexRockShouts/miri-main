# ModelConfig


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **string** |  | [optional] [default to undefined]
**name** | **string** |  | [optional] [default to undefined]
**contextWindow** | **number** |  | [optional] [default to undefined]
**maxTokens** | **number** |  | [optional] [default to undefined]
**reasoning** | **boolean** |  | [optional] [default to undefined]
**input** | **Array&lt;string&gt;** |  | [optional] [default to undefined]
**cost** | [**ModelConfigCost**](ModelConfigCost.md) |  | [optional] [default to undefined]

## Example

```typescript
import { ModelConfig } from './api';

const instance: ModelConfig = {
    id,
    name,
    contextWindow,
    maxTokens,
    reasoning,
    input,
    cost,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
