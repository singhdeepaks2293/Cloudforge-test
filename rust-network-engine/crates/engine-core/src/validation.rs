//! Network / namespace / bridge name validation.
//!
//! Rules (regex-free, std only): non-empty, at most 63 characters, and
//! only lowercase ASCII letters, digits and `-`, starting and ending with
//! an alphanumeric character — i.e. a DNS-label-compatible name, so the
//! same value is safe to use as a Linux interface / namespace name and as
//! a cloud resource name.

use crate::errors::CoreError;

/// Validate a network, namespace or bridge name.
pub fn validate_name(name: &str) -> Result<(), CoreError> {
    if name.is_empty() {
        return Err(CoreError::InvalidArgument(
            "name must not be empty".to_string(),
        ));
    }
    if name.len() > 63 {
        return Err(CoreError::InvalidArgument(format!(
            "name {name:?} exceeds 63 characters"
        )));
    }
    let bytes = name.as_bytes();
    if !bytes[0].is_ascii_alphanumeric() || !bytes[bytes.len() - 1].is_ascii_alphanumeric() {
        return Err(CoreError::InvalidArgument(format!(
            "name {name:?} must start and end with a letter or digit"
        )));
    }
    if !name
        .chars()
        .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-')
    {
        return Err(CoreError::InvalidArgument(format!(
            "name {name:?} may only contain lowercase letters, digits and '-'"
        )));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_names() {
        for name in ["vpc-1", "lab-a", "a", "subnet-01", "br-0", "x9-y8-z7"] {
            assert!(validate_name(name).is_ok(), "{name}");
        }
    }

    #[test]
    fn invalid_names() {
        for name in [
            "",
            "UPPER",
            "has space",
            "has_underscore",
            "-leading",
            "trailing-",
            "-",
            "has.dot",
            "éclair",
        ] {
            assert!(validate_name(name).is_err(), "{name}");
        }
        assert!(validate_name(&"a".repeat(64)).is_err());
        assert!(validate_name(&"a".repeat(63)).is_ok());
    }
}
