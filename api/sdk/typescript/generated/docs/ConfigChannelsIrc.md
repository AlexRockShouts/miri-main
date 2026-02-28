# ConfigChannelsIrc


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**enabled** | **boolean** |  | [optional] [default to undefined]
**host** | **string** |  | [optional] [default to undefined]
**port** | **number** |  | [optional] [default to undefined]
**tls** | **boolean** |  | [optional] [default to undefined]
**nick** | **string** |  | [optional] [default to undefined]
**user** | **string** |  | [optional] [default to undefined]
**realname** | **string** |  | [optional] [default to undefined]
**channels** | **Array&lt;string&gt;** |  | [optional] [default to undefined]
**password** | **string** |  | [optional] [default to undefined]
**nickserv** | [**ConfigChannelsIrcNickserv**](ConfigChannelsIrcNickserv.md) |  | [optional] [default to undefined]
**allowlist** | **Array&lt;string&gt;** |  | [optional] [default to undefined]
**blocklist** | **Array&lt;string&gt;** |  | [optional] [default to undefined]

## Example

```typescript
import { ConfigChannelsIrc } from './api';

const instance: ConfigChannelsIrc = {
    enabled,
    host,
    port,
    tls,
    nick,
    user,
    realname,
    channels,
    password,
    nickserv,
    allowlist,
    blocklist,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
