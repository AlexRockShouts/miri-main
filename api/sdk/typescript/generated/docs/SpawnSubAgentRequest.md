# SpawnSubAgentRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**role** | **string** | Specialist role for the sub-agent | [default to undefined]
**goal** | **string** | Self-contained task description for the sub-agent | [default to undefined]
**model** | **string** | Optional model override (e.g. \&quot;xai/grok-3-mini\&quot;) | [optional] [default to undefined]
**parent_session** | **string** | Parent session ID (defaults to main session) | [optional] [default to undefined]

## Example

```typescript
import { SpawnSubAgentRequest } from './api';

const instance: SpawnSubAgentRequest = {
    role,
    goal,
    model,
    parent_session,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
