## Logging Conventions

The internal logger used in AMKO is uber's Zap logger. It is aliased to gslbutils.[Logf, Errf, Warnf, Debugf]. Following are the conventions for different log levels:

* gslbutils.Errf() - Always an error.

* gslbutils.Warnf() - Something unexpected, but probably not an error.

* gslbutils.Logf() - Generally useful for this to always be visible to an operator.

* gslbutils.Debugf() - Used for development versions which may include extended information about any changes.