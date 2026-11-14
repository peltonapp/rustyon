// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

//! Oracle for the rustyon compatibility suite.
//!
//! Commands:
//!
//! * `serve`: read documents from stdin, each prefixed by its byte length on
//!   its own line, and answer each with one line: `OK <canonical>` when ron-rs
//!   parses it into a `ron::Value`, or `ERR <message>` when it does not.
//! * `fixture`: print the typed fixture serialized by ron-rs, once compact and
//!   once pretty, separated by a line with `---`.
//! * `check`: read RON from stdin, deserialize it into the typed fixture and
//!   exit 0 only if it equals the expected fixture value.

mod fixture;

use std::io::{self, BufRead, Read, Write};

use ron::value::{Number, Value};

fn main() {
    let cmd = std::env::args().nth(1).unwrap_or_default();
    match cmd.as_str() {
        "serve" => serve(),
        "fixture" => fixture::print(),
        "check" => {
            let mut input = String::new();
            io::stdin().read_to_string(&mut input).expect("read stdin");
            std::process::exit(fixture::check(&input));
        }
        _ => {
            eprintln!("usage: ronrs-oracle serve|fixture|check");
            std::process::exit(2);
        }
    }
}

fn serve() {
    let stdin = io::stdin();
    let mut input = stdin.lock();
    let stdout = io::stdout();
    let mut out = stdout.lock();
    let mut header = String::new();
    loop {
        header.clear();
        if input.read_line(&mut header).expect("read header") == 0 {
            return;
        }
        let len: usize = header.trim().parse().expect("length header");
        let mut buf = vec![0u8; len];
        input.read_exact(&mut buf).expect("read document");
        let answer = match std::str::from_utf8(&buf) {
            Err(_) => "ERR invalid UTF-8".to_string(),
            Ok(text) => match ron::from_str::<Value>(text) {
                Ok(v) => format!("OK {}", canonical(&v)),
                Err(e) => format!("ERR {}", e.to_string().replace('\n', " ")),
            },
        };
        writeln!(out, "{answer}").expect("write answer");
        out.flush().expect("flush");
    }
}

/// canonical renders a value in a small, unambiguous text form that the Go
/// side reproduces from rustyon values.
pub fn canonical(v: &Value) -> String {
    let mut s = String::new();
    write_canonical(&mut s, v);
    s
}

fn write_canonical(s: &mut String, v: &Value) {
    match v {
        Value::Unit => s.push_str("unit"),
        Value::Bool(b) => s.push_str(if *b { "true" } else { "false" }),
        Value::Char(c) => s.push_str(&format!("char({})", *c as u32)),
        Value::String(x) => s.push_str(&format!("str({})", hex(x.as_bytes()))),
        Value::Bytes(b) => s.push_str(&format!("bytes({})", hex(b))),
        Value::Number(n) => write_number(s, n),
        Value::Option(None) => s.push_str("none"),
        Value::Option(Some(inner)) => {
            s.push_str("some(");
            write_canonical(s, inner);
            s.push(')');
        }
        Value::Seq(items) => {
            s.push_str("seq[");
            for (i, item) in items.iter().enumerate() {
                if i > 0 {
                    s.push(',');
                }
                write_canonical(s, item);
            }
            s.push(']');
        }
        Value::Map(map) => {
            s.push_str("map{");
            let mut keys: Vec<String> = Vec::new();
            for (i, (k, val)) in map.iter().enumerate() {
                if i > 0 {
                    s.push(',');
                }
                let mut ks = String::new();
                write_canonical(&mut ks, k);
                s.push_str(&ks);
                keys.push(ks);
                s.push(':');
                write_canonical(s, val);
            }
            s.push('}');
            // Keys that print the same but are distinct in ron (for example
            // 1 and 1i8, or NaN and -NaN) cannot be told apart by rustyon, so
            // the comparison treats such documents as accept-only.
            let mut sorted = keys.clone();
            sorted.sort();
            if sorted.windows(2).any(|w| w[0] == w[1]) {
                s.push_str("!dup");
            }
        }
    }
}

fn write_number(s: &mut String, n: &Number) {
    let int = |s: &mut String, v: String| s.push_str(&format!("int({v})"));
    let float = |s: &mut String, f: f64| {
        if f.is_nan() {
            s.push_str("float(nan)");
        } else {
            s.push_str(&format!("float({:016x})", f.to_bits()));
        }
    };
    match n {
        Number::I8(v) => int(s, v.to_string()),
        Number::I16(v) => int(s, v.to_string()),
        Number::I32(v) => int(s, v.to_string()),
        Number::I64(v) => int(s, v.to_string()),
        Number::I128(v) => int(s, v.to_string()),
        Number::U8(v) => int(s, v.to_string()),
        Number::U16(v) => int(s, v.to_string()),
        Number::U32(v) => int(s, v.to_string()),
        Number::U64(v) => int(s, v.to_string()),
        Number::U128(v) => int(s, v.to_string()),
        Number::F32(v) => float(s, f64::from(v.get())),
        Number::F64(v) => float(s, v.get()),
        #[allow(unreachable_patterns)]
        _ => s.push_str("number(?)"),
    }
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}
