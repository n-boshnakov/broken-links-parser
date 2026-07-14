# Test fixture

Inline relative: [guide](../guide/intro.md)

Inline absolute: [repo](https://github.com/org/repo)

Image: ![diagram](./images/arch.png)

Anchor: [see below](#installation)

Reference usage: [guide][ref-guide]

[ref-guide]: https://example.com/guide

Undefined ref (ignored): [missing][no-such-ref]

Code block (ignored):

```
[ignored](http://should-not-appear.com)
```

Inline code (ignored): `[also ignored](http://not-this.com)`

[^1]: This is a footnote definition
[^2]: [OTel Specification](https://opentelemetry.io/docs/specs/)

[defense in depth](https://en.wikipedia.org/wiki/Defense_in_depth_(computing))
