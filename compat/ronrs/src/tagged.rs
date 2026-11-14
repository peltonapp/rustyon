// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

//! Typed fixture for the serde enum representations (internally tagged,
//! adjacently tagged and untagged), used for the round trip tests between
//! ron-rs and rustyon.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Point {
    pub x: i32,
    pub y: i32,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum Internal {
    Empty,
    Circle { radius: f64 },
    Poly { sides: u8, name: String },
    Wrapped(Point),
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "t", content = "c")]
pub enum Adjacent {
    Empty,
    Circle { radius: f64 },
    Rect(u32, u32),
    Label(String),
    Wrapped(Point),
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum Untagged {
    Circle { radius: f64 },
    Poly { sides: u8, name: String },
    Rect(u32, u32),
    Label(String),
    Count(i64),
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Tagged {
    pub internal: Vec<Internal>,
    pub adjacent: Vec<Adjacent>,
    pub untagged: Vec<Untagged>,
}

pub fn expected() -> Tagged {
    Tagged {
        internal: vec![
            Internal::Empty,
            Internal::Circle { radius: 1.5 },
            Internal::Poly {
                sides: 5,
                name: "penta".to_string(),
            },
            Internal::Wrapped(Point { x: 1, y: -2 }),
        ],
        adjacent: vec![
            Adjacent::Empty,
            Adjacent::Circle { radius: 2.5 },
            Adjacent::Rect(3, 4),
            Adjacent::Label("lbl".to_string()),
            Adjacent::Wrapped(Point { x: 5, y: 6 }),
        ],
        untagged: vec![
            Untagged::Circle { radius: 3.5 },
            Untagged::Poly {
                sides: 6,
                name: "hexa".to_string(),
            },
            Untagged::Rect(7, 8),
            Untagged::Label("u".to_string()),
            Untagged::Count(-9),
        ],
    }
}

/// print writes the fixture compact, a separator line, then pretty.
pub fn print() {
    let f = expected();
    let compact = ron::to_string(&f).expect("serialize compact");
    let pretty = ron::ser::to_string_pretty(&f, ron::ser::PrettyConfig::default())
        .expect("serialize pretty");
    println!("{compact}");
    println!("---");
    println!("{pretty}");
}

/// check deserializes input and returns 0 when it equals the expected fixture.
pub fn check(input: &str) -> i32 {
    match ron::from_str::<Tagged>(input) {
        Ok(got) => {
            let want = expected();
            if got == want {
                0
            } else {
                eprintln!("tagged mismatch\n got: {got:?}\nwant: {want:?}");
                1
            }
        }
        Err(e) => {
            eprintln!("deserialize error: {e}");
            1
        }
    }
}
